package provider

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
)

// ---------------------------------------------------------------------------
// Provider-agnostic tool types
// ---------------------------------------------------------------------------

// ToolSpec is a provider-agnostic tool definition. Each provider converts
// these to its own wire format.
type ToolSpec struct {
	Name        string
	Description string
	Properties  map[string]ToolProp
	Required    []string

	// DeferLoading marks this tool for deferred loading via Tool Search.
	// Deferred tools are excluded from initial context and loaded on demand.
	DeferLoading bool

	// AllowedCallers controls who can invoke this tool.
	// Values: "direct" (standard), "code_execution_20250825" (PTC).
	// Empty means default (direct only).
	AllowedCallers []string
}

// ToolProp describes a single tool input property.
type ToolProp struct {
	Type        string
	Description string
	Enum        []string
	// Items describes the element schema when Type is "array".
	Items *ToolProp
	// Properties describes nested object properties (when Type is "object" or
	// Items.Type is "object").
	Properties map[string]ToolProp
	// Required lists required fields when this prop describes an object.
	Required []string
}

// Usage contains token accounting for a streamed model call.
type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// ---------------------------------------------------------------------------
// Provider interface
// ---------------------------------------------------------------------------

// Provider is the interface that each LLM provider implements.
type Provider interface {
	// StreamMessage sends a message to the provider's API with streaming.
	// onDelta is called for each text chunk received.
	// Returns content blocks, stop reason, input tokens, output tokens, error.
	StreamMessage(
		apiKey, modelID string,
		history []domain.TranscriptMessage,
		tools []ToolSpec,
		system string,
		onDelta func(string),
	) ([]domain.ContentBlock, string, Usage, error)

	// FetchModels retrieves the list of available models.
	FetchModels(apiKey string) ([]domain.APIModelInfo, error)

	// Name returns the provider name (e.g. "anthropic", "openai").
	Name() string
}

// ---------------------------------------------------------------------------
// Provider registry
// ---------------------------------------------------------------------------

// GetProvider returns a Provider implementation by name.
func GetProvider(name string) (Provider, error) {
	switch strings.ToLower(name) {
	case "":
		return nil, fmt.Errorf("no provider specified; use /config set model <provider>/<model>")
	case "anthropic":
		return &AnthropicProvider{}, nil
	case "zai":
		return &ZAIProvider{}, nil
	case "grok":
		return &GrokProvider{}, nil
	case "mistral":
		return &MistralProvider{}, nil
	case "openai":
		return &OpenAIProvider{}, nil
	case "ollama":
		return &OllamaProvider{}, nil
	case "fireworks":
		return &FireworksProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: anthropic, zai, grok, mistral, openai, ollama, fireworks)", name)
	}
}

// ---------------------------------------------------------------------------
// Model resolution
// ---------------------------------------------------------------------------

// ResolveProviderAndModel parses a model specifier like "openai/gpt-4o" or
// "claude-sonnet" into a (provider, modelID) pair.
//
// Rules:
//   - "openai/gpt-4o" -> ("openai", "gpt-4o")
//   - "anthropic/claude-sonnet" -> ("anthropic", resolved alias)
//   - "claude-sonnet" -> ("anthropic", resolved alias) -- known Anthropic alias
//   - "gpt-4o" -> (currentProvider, "gpt-4o") -- bare unknown name
func ResolveProviderAndModel(spec string, currentProvider string) (string, string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return currentProvider, ""
	}

	// Check for explicit provider/ prefix
	if idx := strings.Index(spec, "/"); idx > 0 {
		prefix := strings.ToLower(spec[:idx])
		model := spec[idx+1:]
		switch prefix {
		case "anthropic":
			return "anthropic", ResolveModel(model)
		case "zai", "zai-sdk", "zhipu":
			return "zai", model
		case "grok", "xai":
			return "grok", model
		case "mistral", "openai", "google", "ollama", "fireworks":
			return prefix, model
		}
		// Unknown prefix (e.g. "accounts/fireworks/models/...") --
		// scan all path segments for a known provider name
		lower := strings.ToLower(spec)
		for _, prov := range []string{"fireworks", "anthropic", "openai", "mistral", "google", "grok", "ollama", "zai"} {
			if strings.Contains(lower, "/"+prov+"/") || strings.HasPrefix(lower, prov+"/") {
				return prov, spec
			}
		}
		// No known provider found -- treat as full model name with current provider
	}

	// Handle "anthropic.model-id" (dot separator instead of slash)
	if strings.HasPrefix(strings.ToLower(spec), "anthropic.") {
		model := spec[len("anthropic."):]
		return "anthropic", ResolveModel(model)
	}

	// Bare name: check if it's a known Anthropic alias
	lower := strings.ToLower(spec)
	if _, ok := ModelAliases[lower]; ok {
		return "anthropic", ResolveModel(spec)
	}

	// Check if it looks like an Anthropic model ID
	if strings.HasPrefix(lower, "claude-") {
		return "anthropic", ResolveModel(spec)
	}

	// Check if it looks like an OpenAI model
	if strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1") ||
		strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") {
		return "openai", spec
	}

	// Mistral family model IDs
	if strings.HasPrefix(lower, "mistral-") ||
		strings.HasPrefix(lower, "ministral-") ||
		strings.HasPrefix(lower, "codestral-") ||
		strings.HasPrefix(lower, "pixtral-") ||
		strings.HasPrefix(lower, "open-mistral-") {
		return "mistral", spec
	}

	// Grok/xAI model IDs.
	if strings.HasPrefix(lower, "grok-") {
		return "grok", spec
	}

	// Z.AI GLM family model IDs.
	if strings.HasPrefix(lower, "glm-") || strings.HasPrefix(lower, "autoglm-") {
		return "zai", spec
	}

	// Fireworks model IDs use "accounts/fireworks/models/" prefix.
	if strings.HasPrefix(lower, "accounts/fireworks/") {
		return "fireworks", spec
	}

	// Ollama/local model IDs often contain a tag suffix (e.g. "gemma3:4b").
	if strings.Contains(spec, ":") {
		return "ollama", spec
	}

	// Unknown -- use current provider
	return currentProvider, spec
}
