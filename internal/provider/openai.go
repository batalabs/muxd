package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/domain"
)

// ---------------------------------------------------------------------------
// OpenAIProvider — implements Provider for the OpenAI API
// ---------------------------------------------------------------------------

// OpenAIProvider implements Provider for the OpenAI API.
type OpenAIProvider struct{}

// Name returns "openai".
func (p *OpenAIProvider) Name() string { return "openai" }

// ---------------------------------------------------------------------------
// FetchModels
// ---------------------------------------------------------------------------

// FetchModels retrieves the list of available models from the OpenAI API.
func (p *OpenAIProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	httpReq, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var listResp struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	var models []domain.APIModelInfo
	for _, m := range listResp.Data {
		if isOpenAIChatModel(m.ID) {
			models = append(models, domain.APIModelInfo{
				ID:          m.ID,
				DisplayName: m.ID,
			})
		}
	}
	return models, nil
}

// isOpenAIChatModel returns true for model IDs that are chat-capable.
func isOpenAIChatModel(id string) bool {
	lower := strings.ToLower(id)
	prefixes := []string{"gpt-4", "gpt-3.5", "o1", "o3", "o4"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// StreamMessage
// ---------------------------------------------------------------------------

// StreamMessage sends a message to the OpenAI API with streaming.
func (p *OpenAIProvider) StreamMessage(
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	msgs := buildOpenAIMessages(history, system)

	streamOpts := &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}

	reqBody := openaiRequest{
		Model:         modelID,
		Messages:      msgs,
		Stream:        true,
		Tools:         toOpenAITools(tools),
		StreamOptions: streamOpts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	// Prevent proxies from injecting compression on the SSE stream.
	httpReq.Header.Set("Accept-Encoding", "identity")

	resp, err := streamHTTPClient.Do(httpReq)
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		errType := ""
		errMessage := fmt.Sprintf("HTTP %d", resp.StatusCode)
		var errResp struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &errResp) == nil && errResp.Error != nil {
			errType = errResp.Error.Type
			errMessage = errResp.Error.Message
		}
		return nil, "", Usage{}, NewAPIError(resp.StatusCode, errType, errMessage, resp.Header)
	}

	return parseOpenAISSE(resp.Body, onDelta)
}

// ---------------------------------------------------------------------------
// OpenAI wire types
// ---------------------------------------------------------------------------

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiRequest struct {
	Model         string          `json:"model"`
	Messages      []openaiMessage `json:"messages"`
	Stream        bool            `json:"stream"`
	Tools         []openaiTool    `json:"tools,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

// ---------------------------------------------------------------------------
// Message conversion
// ---------------------------------------------------------------------------

// buildOpenAIMessages converts transcript messages to OpenAI API format.
func buildOpenAIMessages(history []domain.TranscriptMessage, system string) []openaiMessage {
	msgs := make([]openaiMessage, 0, len(history)+1)

	// System prompt as first message
	if system != "" {
		raw, _ := json.Marshal(system)
		msgs = append(msgs, openaiMessage{Role: "system", Content: raw})
	}

	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}

		if m.HasBlocks() {
			// Check if this is a tool results message (user role with tool_result blocks)
			if m.Role == "user" {
				for _, b := range m.Blocks {
					if b.Type == "tool_result" {
						result := b.ToolResult
						// Truncate old tool results to reduce context size.
						const maxToolResult = 10000
						if len(result) > maxToolResult {
							result = result[:maxToolResult] + "\n... (truncated for context)"
						}
						content, _ := json.Marshal(result)
						msgs = append(msgs, openaiMessage{
							Role:       "tool",
							Content:    content,
							ToolCallID: b.ToolUseID,
						})
					}
				}
				continue
			}

			// Assistant message with tool calls
			var textParts []string
			var toolCalls []openaiToolCall
			for _, b := range m.Blocks {
				switch b.Type {
				case "text":
					textParts = append(textParts, b.Text)
				case "tool_use":
					toolInput := b.ToolInput
					if toolInput == nil {
						toolInput = map[string]any{}
					}
					argsJSON, _ := json.Marshal(toolInput)
					tc := openaiToolCall{
						ID:   b.ToolUseID,
						Type: "function",
					}
					tc.Function.Name = b.ToolName
					tc.Function.Arguments = string(argsJSON)
					toolCalls = append(toolCalls, tc)
				}
			}

			msg := openaiMessage{Role: "assistant"}
			if len(textParts) > 0 {
				text := strings.Join(textParts, "\n")
				raw, _ := json.Marshal(text)
				msg.Content = raw
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			msgs = append(msgs, msg)
		} else {
			raw, _ := json.Marshal(m.Content)
			msgs = append(msgs, openaiMessage{Role: m.Role, Content: raw})
		}
	}

	return msgs
}

// convertOpenAIProp recursively converts a ToolProp to an OpenAI-compatible map.
func convertOpenAIProp(v ToolProp) map[string]any {
	prop := map[string]any{
		"type": v.Type,
	}
	if v.Description != "" {
		prop["description"] = v.Description
	}
	if len(v.Enum) > 0 {
		prop["enum"] = v.Enum
	}
	if v.Items != nil {
		prop["items"] = convertOpenAIProp(*v.Items)
	}
	if len(v.Properties) > 0 {
		nested := make(map[string]any, len(v.Properties))
		for k, np := range v.Properties {
			nested[k] = convertOpenAIProp(np)
		}
		prop["properties"] = nested
	}
	if len(v.Required) > 0 {
		prop["required"] = v.Required
	}
	return prop
}

// toOpenAITools converts provider-agnostic ToolSpecs to OpenAI wire format.
func toOpenAITools(specs []ToolSpec) []openaiTool {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]openaiTool, len(specs))
	for i, s := range specs {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = convertOpenAIProp(v)
		}
		req := s.Required
		if req == nil {
			req = []string{}
		}
		params := map[string]any{
			"type":       "object",
			"properties": props,
			"required":   req,
		}
		paramsJSON, _ := json.Marshal(params)
		tools[i] = openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  paramsJSON,
			},
		}
	}
	return tools
}

// ---------------------------------------------------------------------------
// SSE parsing
// ---------------------------------------------------------------------------

type openaiSSEDelta struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string               `json:"role"`
			Content   string               `json:"content"`
			ToolCalls []openaiSSEToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openaiSSEToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// parseOpenAISSE parses the OpenAI SSE stream and returns content blocks.
func parseOpenAISSE(body io.Reader, onDelta func(string)) ([]domain.ContentBlock, string, Usage, error) {
	var textBuf strings.Builder
	toolBuilders := make(map[int]*openaiToolBuilder)
	usage := Usage{}
	finishReason := ""

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line size
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openaiSSEDelta
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}

		// Token usage
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}

		for _, choice := range chunk.Choices {
			// Text content
			if choice.Delta.Content != "" {
				textBuf.WriteString(choice.Delta.Content)
				if onDelta != nil {
					onDelta(choice.Delta.Content)
				}
			}

			// Tool calls
			for _, tc := range choice.Delta.ToolCalls {
				builder, ok := toolBuilders[tc.Index]
				if !ok {
					builder = &openaiToolBuilder{}
					toolBuilders[tc.Index] = builder
				}
				if tc.ID != "" {
					builder.id = tc.ID
				}
				if tc.Function.Name != "" {
					builder.name = tc.Function.Name
				}
				builder.args.WriteString(tc.Function.Arguments)
			}

			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}
	}

	// Build content blocks
	var blocks []domain.ContentBlock
	if text := textBuf.String(); text != "" {
		blocks = append(blocks, domain.ContentBlock{Type: "text", Text: text})
	}

	// Sort tool builders by index and append
	for i := 0; i < len(toolBuilders); i++ {
		builder, ok := toolBuilders[i]
		if !ok {
			continue
		}
		input := map[string]any{}
		if argsStr := builder.args.String(); argsStr != "" {
			if err := json.Unmarshal([]byte(argsStr), &input); err != nil {
				fmt.Fprintf(os.Stderr, "openai: unmarshal tool args: %v\n", err)
			}
		}
		blocks = append(blocks, domain.ContentBlock{
			Type:      "tool_use",
			ToolUseID: builder.id,
			ToolName:  builder.name,
			ToolInput: input,
		})
	}

	// Normalize stop reason
	stopReason := normalizeOpenAIStop(finishReason)

	if err := scanner.Err(); err != nil && finishReason == "" {
		return nil, "", usage, fmt.Errorf("reading stream: %w", err)
	}

	return blocks, stopReason, usage, nil
}

type openaiToolBuilder struct {
	id   string
	name string
	args strings.Builder
}

// normalizeOpenAIStop maps OpenAI finish reasons to Anthropic-style stop reasons.
func normalizeOpenAIStop(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "end_turn"
	default:
		if reason == "" {
			return "end_turn"
		}
		return reason
	}
}
