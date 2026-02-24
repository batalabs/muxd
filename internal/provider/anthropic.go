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
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/domain"
)

// streamHTTPClient is shared across all streaming API calls (Anthropic + OpenAI).
// A single shared Transport reuses connections and avoids ephemeral port
// exhaustion on Windows. DisableCompression prevents gzip-over-chunked
// encoding failures. TLSNextProto is left nil so Go auto-negotiates HTTP/2,
// which uses its own binary framing instead of chunked transfer encoding —
// avoiding Go 1.25+'s strict bare-LF rejection (CVE-2025-22871).
var streamHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   4,
	},
}

// CloseIdleConnections drops all idle connections from the shared HTTP
// transport. Call before retrying after a stream error so the next attempt
// gets a fresh TCP/TLS connection instead of reusing a stale pooled one.
// (Go's Transport auto-retries stale connections only for idempotent methods;
// our POST requests don't benefit from that.)
func CloseIdleConnections() {
	streamHTTPClient.CloseIdleConnections()
}

// ---------------------------------------------------------------------------
// Test hook and constants
// ---------------------------------------------------------------------------

// TestAPIURL is overridden in tests to point at a local httptest server.
var TestAPIURL string

// AnthropicMessagesURL is the default Anthropic Messages API endpoint.
const AnthropicMessagesURL = "https://api.anthropic.com/v1/messages"

// ---------------------------------------------------------------------------
// StreamMessagePure — delegates to AnthropicProvider for SSE streaming
// ---------------------------------------------------------------------------

// StreamMessagePure calls the Anthropic API using the default URL or
// TestAPIURL override. Used by AgentService when no explicit provider is set.
func StreamMessagePure(
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	url := AnthropicMessagesURL
	if TestAPIURL != "" {
		url = TestAPIURL
	}
	return StreamMessagePureWithURL(url, apiKey, modelID, history, tools, system, onDelta)
}

// StreamMessagePureWithURL is the implementation that accepts an explicit URL,
// making it testable with httptest servers.
func StreamMessagePureWithURL(
	apiURL string,
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	blocks, stopReason, usage, _, err := anthropicStreamWithURL(
		apiURL, apiKey, modelID, history, tools, system, onDelta, "",
	)
	return blocks, stopReason, usage, err
}

// ---------------------------------------------------------------------------
// AnthropicProvider — implements Provider for the Anthropic API
// ---------------------------------------------------------------------------

// AnthropicProvider implements Provider for the Anthropic API.
// Stateful: tracks the PTC container ID for reuse across turns.
type AnthropicProvider struct {
	mu          sync.Mutex
	containerID string // PTC container ID, empty if not active
}

// Name returns "anthropic".
func (p *AnthropicProvider) Name() string { return "anthropic" }

// FetchModels retrieves the list of available models from the Anthropic API.
func (p *AnthropicProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	httpReq, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/models?limit=100", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		var errResp struct {
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &errResp) == nil && errResp.Error != nil {
			errMsg = fmt.Sprintf("%s: %s", errResp.Error.Type, errResp.Error.Message)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	var listResp modelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return listResp.Data, nil
}

// StreamMessage sends a message to the Anthropic API with streaming.
func (p *AnthropicProvider) StreamMessage(
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	p.mu.Lock()
	containerID := p.containerID
	p.mu.Unlock()

	blocks, stopReason, usage, newContainer, err := anthropicStreamWithURL(
		AnthropicMessagesURL, apiKey, modelID, history, tools, system, onDelta, containerID,
	)

	if newContainer != "" {
		p.mu.Lock()
		p.containerID = newContainer
		p.mu.Unlock()
	}

	return blocks, stopReason, usage, err
}

// ---------------------------------------------------------------------------
// Anthropic wire types
// ---------------------------------------------------------------------------

type modelsListResponse struct {
	Data    []domain.APIModelInfo `json:"data"`
	HasMore bool                  `json:"has_more"`
	LastID  string                `json:"last_id"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// newTextMessage creates an anthropicMessage with a plain text content string.
func newTextMessage(role, text string) anthropicMessage {
	raw, _ := json.Marshal(text)
	return anthropicMessage{Role: role, Content: raw}
}

// newBlockMessage creates an anthropicMessage with an array of content blocks.
func newBlockMessage(role string, blocks []anthropicContentBlock) anthropicMessage {
	raw, _ := json.Marshal(blocks)
	return anthropicMessage{Role: role, Content: raw}
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     *map[string]any `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   *string         `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
}

// anthropicCacheControl marks a block for ephemeral prompt caching.
// Cached blocks are charged at ~10% on subsequent requests within 5 minutes.
type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// anthropicSystemBlock is a content block in the system message array.
// Using an array (instead of a plain string) enables cache_control.
type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicToolItem is a union type for standard and special Anthropic tools.
// Standard tools: Name + Description + InputSchema (+ optional AllowedCallers, DeferLoading).
// Special tools (PTC, Tool Search): Type + Name only.
type anthropicToolItem struct {
	// Required for all tools
	Name string `json:"name"`

	// Standard tool fields (omitted for special tools via omitempty)
	Description string               `json:"description,omitempty"`
	InputSchema *anthropicToolSchema `json:"input_schema,omitempty"`

	// Advanced tool use fields
	Type           string   `json:"type,omitempty"` // e.g., "code_execution_20250825"
	AllowedCallers []string `json:"allowed_callers,omitempty"`
	DeferLoading   *bool    `json:"defer_loading,omitempty"`

	// Prompt caching
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicToolSchema struct {
	Type       string                       `json:"type"`
	Properties map[string]anthropicToolProp `json:"properties"`
	Required   []string                     `json:"required"`
}

type anthropicToolProp struct {
	Type        string                       `json:"type"`
	Description string                       `json:"description,omitempty"`
	Enum        []string                     `json:"enum,omitempty"`
	Items       *anthropicToolProp           `json:"items,omitempty"`
	Properties  map[string]anthropicToolProp `json:"properties,omitempty"`
	Required    []string                     `json:"required,omitempty"`
}

type anthropicRequest struct {
	Model             string                 `json:"model"`
	MaxTokens         int                    `json:"max_tokens"`
	Messages          []anthropicMessage     `json:"messages"`
	Stream            bool                   `json:"stream"`
	Tools             []anthropicToolItem    `json:"tools,omitempty"`
	System            []anthropicSystemBlock `json:"system,omitempty"`
	Container         string                 `json:"container,omitempty"` // PTC container reuse
	ContextManagement *anthropicContextMgmt  `json:"context_management,omitempty"`
}

// anthropicContextMgmt enables server-side context management features.
type anthropicContextMgmt struct {
	Edits []anthropicContextEdit `json:"edits"`
}

// anthropicContextEdit configures a context management strategy.
type anthropicContextEdit struct {
	Type    string                      `json:"type"`
	Trigger *anthropicCompactionTrigger `json:"trigger,omitempty"`
}

// anthropicCompactionTrigger configures when compaction fires.
type anthropicCompactionTrigger struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

// ---------------------------------------------------------------------------
// SSE event types
// ---------------------------------------------------------------------------

type sseEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock *struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Name   string `json:"name"`
		Caller *struct {
			Type   string `json:"type"`
			ToolID string `json:"tool_id"`
		} `json:"caller"`
		// ToolUseID for code_execution_tool_result blocks.
		ToolUseID string `json:"tool_use_id"`
		// Content for tool_search_tool_result and code_execution_tool_result.
		Content json.RawMessage `json:"content"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
		Content     string `json:"content"` // compaction_delta
	} `json:"delta"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	Message *struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
		Container *struct {
			ID        string `json:"id"`
			ExpiresAt string `json:"expires_at"`
		} `json:"container"`
	} `json:"message"`
	// Error is populated for SSE error events sent mid-stream.
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// codeExecutionContent is the inner content of a code_execution_tool_result block.
type codeExecutionContent struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ReturnCode int    `json:"return_code"`
}

// streamBlock tracks an in-flight content block during SSE streaming.
type streamBlock struct {
	blockType    string
	toolID       string
	toolName     string
	textBuf      strings.Builder
	jsonBuf      strings.Builder
	callerType   string // PTC: "code_execution_20250825" or empty for direct
	callerToolID string // PTC: the server_tool_use ID that spawned this call
}

// ---------------------------------------------------------------------------
// Tool conversion
// ---------------------------------------------------------------------------

// convertAnthropicProp recursively converts a ToolProp to anthropicToolProp.
func convertAnthropicProp(v ToolProp) anthropicToolProp {
	p := anthropicToolProp{
		Type:        v.Type,
		Description: v.Description,
		Enum:        v.Enum,
	}
	if v.Items != nil {
		converted := convertAnthropicProp(*v.Items)
		p.Items = &converted
	}
	if len(v.Properties) > 0 {
		p.Properties = make(map[string]anthropicToolProp, len(v.Properties))
		for k, nested := range v.Properties {
			p.Properties[k] = convertAnthropicProp(nested)
		}
	}
	if len(v.Required) > 0 {
		p.Required = v.Required
	}
	return p
}

// supportsAdvancedTools reports whether a model supports PTC and Tool Search.
// Haiku does not. Only Opus 4.5+ and Sonnet 4.5+ do.
func supportsAdvancedTools(modelID string) bool {
	lower := strings.ToLower(modelID)
	if strings.Contains(lower, "haiku") {
		return false
	}
	return strings.Contains(lower, "opus") || strings.Contains(lower, "sonnet")
}

// toAnthropicTools converts provider-agnostic ToolSpecs to Anthropic wire format,
// prepending special tools for PTC (code_execution) and Tool Search when the
// model supports them.
func toAnthropicTools(specs []ToolSpec, modelID string) []anthropicToolItem {
	if len(specs) == 0 {
		return nil
	}

	advanced := supportsAdvancedTools(modelID)

	// Check if any tools support PTC or deferred loading.
	hasPTC := false
	hasDeferred := false
	if advanced {
		for _, s := range specs {
			for _, c := range s.AllowedCallers {
				if c == "code_execution_20250825" {
					hasPTC = true
				}
			}
			if s.DeferLoading {
				hasDeferred = true
			}
		}
	}

	items := make([]anthropicToolItem, 0, len(specs)+2)

	// Add code execution tool if any tool supports PTC.
	if hasPTC {
		items = append(items, anthropicToolItem{
			Type: "code_execution_20250825",
			Name: "code_execution",
		})
	}

	// Add tool search tool if any tool is deferred.
	if hasDeferred {
		items = append(items, anthropicToolItem{
			Type: "tool_search_tool_regex_20251119",
			Name: "tool_search_tool_regex",
		})
	}

	// Add standard tools.
	for _, s := range specs {
		props := make(map[string]anthropicToolProp, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = convertAnthropicProp(v)
		}
		req := s.Required
		if req == nil {
			req = []string{}
		}
		schema := &anthropicToolSchema{
			Type:       "object",
			Properties: props,
			Required:   req,
		}
		item := anthropicToolItem{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: schema,
		}
		if advanced && len(s.AllowedCallers) > 0 {
			item.AllowedCallers = s.AllowedCallers
		}
		if advanced && s.DeferLoading {
			dl := true
			item.DeferLoading = &dl
		}
		items = append(items, item)
	}

	// Mark the last tool with cache_control so the entire tool list is
	// cached as a prefix. Subsequent requests within 5 minutes pay ~10%
	// for the cached portion instead of full input token price.
	if len(items) > 0 {
		items[len(items)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}

	return items
}

// ---------------------------------------------------------------------------
// Streaming implementation
// ---------------------------------------------------------------------------

// anthropicStreamWithURL is the core streaming implementation for Anthropic.
// containerID is the PTC container ID for reuse (empty on first call).
// Returns (blocks, stopReason, inputTokens, outputTokens, newContainerID, error).
func anthropicStreamWithURL(
	apiURL string,
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
	containerID string,
) ([]domain.ContentBlock, string, Usage, string, error) {
	msgs := buildAnthropicMessages(history)

	// System prompt as a cached content block array.
	var systemBlocks []anthropicSystemBlock
	if system != "" {
		systemBlocks = []anthropicSystemBlock{
			{
				Type:         "text",
				Text:         system,
				CacheControl: &anthropicCacheControl{Type: "ephemeral"},
			},
		}
	}

	reqBody := anthropicRequest{
		Model:     modelID,
		MaxTokens: 16384,
		Messages:  msgs,
		Stream:    true,
		Tools:     toAnthropicTools(tools, modelID),
		System:    systemBlocks,
		Container: containerID,
	}

	// Server-side compaction only for models that support it (not Haiku).
	if supportsAdvancedTools(modelID) {
		reqBody.ContextManagement = &anthropicContextMgmt{
			Edits: []anthropicContextEdit{
				{Type: "compact_20260112"},
			},
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", Usage{}, "", fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", Usage{}, "", fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Beta headers: prompt caching is always on; advanced features only for capable models.
	beta := "prompt-caching-2024-07-31"
	if supportsAdvancedTools(modelID) {
		beta += ",compact-2026-01-12,advanced-tool-use-2025-11-20"
	}
	httpReq.Header.Set("anthropic-beta", beta)

	// Prevent proxies from injecting compression on the SSE stream.
	httpReq.Header.Set("Accept-Encoding", "identity")

	resp, err := streamHTTPClient.Do(httpReq)
	if err != nil {
		return nil, "", Usage{}, "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		errType := ""
		errMessage := fmt.Sprintf("HTTP %d", resp.StatusCode)
		var errResp struct {
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &errResp) == nil && errResp.Error != nil {
			errType = errResp.Error.Type
			errMessage = errResp.Error.Message
		}
		return nil, "", Usage{}, "", NewAPIError(resp.StatusCode, errType, errMessage, resp.Header)
	}

	blocks, stopReason, usage, newContainer, sseErr := parseAnthropicSSE(&lenientReader{r: resp.Body}, onDelta)
	return blocks, stopReason, usage, newContainer, sseErr
}

// lenientReader wraps an io.Reader and absorbs transport-level errors
// (chunked encoding issues from TLS-intercepting proxies, connection resets)
// by converting them to io.EOF. This ensures the SSE parser processes all
// data that was successfully received before the error occurred.
type lenientReader struct {
	r   io.Reader
	err error // saved transport error, nil if clean
}

func (lr *lenientReader) Read(p []byte) (int, error) {
	n, err := lr.r.Read(p)
	if err != nil && err != io.EOF {
		// Transport error — save it and return what we got.
		lr.err = err
		if n > 0 {
			return n, nil // deliver buffered data, suppress error for now
		}
		return 0, io.EOF // no data left, signal clean EOF
	}
	return n, err
}

// ---------------------------------------------------------------------------
// Message conversion
// ---------------------------------------------------------------------------

// buildAnthropicMessages converts transcript messages to Anthropic API format.
func buildAnthropicMessages(history []domain.TranscriptMessage) []anthropicMessage {
	msgs := make([]anthropicMessage, 0, len(history))
	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.HasBlocks() {
			apiBlocks := make([]anthropicContentBlock, 0, len(m.Blocks))
			for _, b := range m.Blocks {
				switch b.Type {
				case "text":
					apiBlocks = append(apiBlocks, anthropicContentBlock{Type: "text", Text: b.Text})
				case "compaction":
					content := b.Text
					apiBlocks = append(apiBlocks, anthropicContentBlock{Type: "compaction", Content: &content})
				case "tool_use":
					input := b.ToolInput
					if input == nil {
						input = map[string]any{}
					}
					apiBlocks = append(apiBlocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    b.ToolUseID,
						Name:  b.ToolName,
						Input: &input,
					})
				case "tool_result":
					content := b.ToolResult
					// Truncate old tool results to reduce context size.
					const maxToolResult = 10000
					if len(content) > maxToolResult {
						content = content[:maxToolResult] + "\n... (truncated for context)"
					}
					block := anthropicContentBlock{
						Type:      "tool_result",
						ToolUseID: b.ToolUseID,
						Content:   &content,
					}
					if b.IsError {
						isErr := true
						block.IsError = &isErr
					}
					apiBlocks = append(apiBlocks, block)
				}
			}
			msgs = append(msgs, newBlockMessage(m.Role, apiBlocks))
		} else {
			msgs = append(msgs, newTextMessage(m.Role, m.Content))
		}
	}
	return msgs
}

// ---------------------------------------------------------------------------
// SSE parsing
// ---------------------------------------------------------------------------

// parseAnthropicSSE parses the Anthropic SSE stream and returns content blocks.
// The body should be a *lenientReader so transport errors (chunked encoding,
// connection resets) are absorbed and all buffered data is processed.
// Returns (blocks, stopReason, inputTokens, outputTokens, containerID, error).
func parseAnthropicSSE(body io.Reader, onDelta func(string)) ([]domain.ContentBlock, string, Usage, string, error) {
	var blocks []streamBlock
	usage := Usage{}
	stopReason := ""
	containerID := ""

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line size
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event sseEvent
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}

		switch event.Type {
		case "error":
			// Mid-stream error from the API (e.g., overloaded_error).
			errType := ""
			errMsg := "unknown API error"
			if event.Error != nil {
				errType = event.Error.Type
				errMsg = event.Error.Message
			}
			return assembleBlocks(blocks), stopReason, usage, containerID,
				&APIError{StatusCode: 0, ErrorType: errType, Message: errMsg}

		case "message_start":
			if event.Message != nil {
				usage.InputTokens = event.Message.Usage.InputTokens
				usage.CacheCreationInputTokens = event.Message.Usage.CacheCreationInputTokens
				usage.CacheReadInputTokens = event.Message.Usage.CacheReadInputTokens
				if event.Message.Container != nil {
					containerID = event.Message.Container.ID
				}
			}

		case "content_block_start":
			sb := streamBlock{}
			if event.ContentBlock != nil {
				sb.blockType = event.ContentBlock.Type
				sb.toolID = event.ContentBlock.ID
				sb.toolName = event.ContentBlock.Name

				// PTC caller tracking.
				if event.ContentBlock.Caller != nil {
					sb.callerType = event.ContentBlock.Caller.Type
					sb.callerToolID = event.ContentBlock.Caller.ToolID
				}

				// code_execution_tool_result: extract stdout from inline content.
				if sb.blockType == "code_execution_tool_result" && len(event.ContentBlock.Content) > 0 {
					var execResult codeExecutionContent
					if json.Unmarshal(event.ContentBlock.Content, &execResult) == nil {
						sb.textBuf.WriteString(execResult.Stdout)
					}
				}
			}
			for len(blocks) <= event.Index {
				blocks = append(blocks, streamBlock{})
			}
			blocks[event.Index] = sb

		case "content_block_delta":
			if event.Index < len(blocks) {
				switch event.Delta.Type {
				case "text_delta":
					blocks[event.Index].textBuf.WriteString(event.Delta.Text)
					if onDelta != nil {
						onDelta(event.Delta.Text)
					}
				case "input_json_delta":
					blocks[event.Index].jsonBuf.WriteString(event.Delta.PartialJSON)
				case "compaction_delta":
					// Single delta with the complete compaction summary.
					blocks[event.Index].textBuf.WriteString(event.Delta.Content)
				}
			}

		case "message_delta":
			if event.Usage.OutputTokens > 0 {
				usage.OutputTokens = event.Usage.OutputTokens
			}
			if event.Delta.StopReason != "" {
				stopReason = event.Delta.StopReason
			}
		}
	}

	// Check for transport errors saved by lenientReader. If the API already
	// sent a stop_reason, the response is complete — ignore the error.
	// Also ignore if we got content blocks (partial response is better than
	// no response for the retry logic to handle).
	var transportErr error
	if lr, ok := body.(*lenientReader); ok {
		transportErr = lr.err
	}
	if scanErr := scanner.Err(); scanErr != nil {
		transportErr = scanErr
	}

	if transportErr != nil && stopReason == "" {
		// If we have a text-only response (no tool_use), salvage it as a
		// normal end_turn instead of surfacing transport noise to users.
		// For tool_use turns we still fail/retry, because partial JSON is unsafe.
		assembled := assembleBlocks(blocks)
		if len(assembled) > 0 && !hasToolUseBlock(assembled) {
			return assembled, "end_turn", usage, containerID, nil
		}

		// Stream died before completion (no stop_reason received).
		// Propagate error so the retry logic gets a chance, even if we
		// received partial blocks — partial tool_use with incomplete JSON
		// is worse than a clean retry.
		return nil, "", usage, containerID, fmt.Errorf("reading stream: %w", transportErr)
	}

	return assembleBlocks(blocks), stopReason, usage, containerID, nil
}

func hasToolUseBlock(blocks []domain.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// assembleBlocks converts streamBlocks into domain.ContentBlocks.
// Skips server-side blocks (server_tool_use, tool_search_tool_result) that
// the client does not need to act on. Includes code_execution_tool_result
// stdout as informational text.
func assembleBlocks(blocks []streamBlock) []domain.ContentBlock {
	var contentBlocks []domain.ContentBlock
	for _, sb := range blocks {
		switch sb.blockType {
		case "text":
			contentBlocks = append(contentBlocks, domain.ContentBlock{
				Type: "text",
				Text: sb.textBuf.String(),
			})
		case "tool_use":
			input := map[string]any{}
			if jsonStr := sb.jsonBuf.String(); jsonStr != "" {
				if err := json.Unmarshal([]byte(jsonStr), &input); err != nil {
					fmt.Fprintf(os.Stderr, "anthropic: unmarshal tool input: %v\n", err)
				}
			}
			block := domain.ContentBlock{
				Type:      "tool_use",
				ToolUseID: sb.toolID,
				ToolName:  sb.toolName,
				ToolInput: input,
			}
			if sb.callerType != "" {
				block.CallerType = sb.callerType
				block.CallerToolID = sb.callerToolID
			}
			contentBlocks = append(contentBlocks, block)
		case "compaction":
			contentBlocks = append(contentBlocks, domain.ContentBlock{
				Type: "compaction",
				Text: sb.textBuf.String(),
			})
		case "code_execution_tool_result":
			// Include PTC code output as informational text.
			if stdout := sb.textBuf.String(); stdout != "" {
				contentBlocks = append(contentBlocks, domain.ContentBlock{
					Type: "text",
					Text: stdout,
				})
			}
			// server_tool_use, tool_search_tool_result: skip (server-side only).
		}
	}
	return contentBlocks
}
