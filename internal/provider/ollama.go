package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
)

var ollamaBaseURL = "http://localhost:11434"

// SetOllamaBaseURL configures the Ollama endpoint.
func SetOllamaBaseURL(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		ollamaBaseURL = "http://localhost:11434"
		return
	}
	ollamaBaseURL = strings.TrimRight(raw, "/")
}

type OllamaProvider struct{}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) FetchModels(_ string) ([]domain.APIModelInfo, error) {
	req, err := http.NewRequest(http.MethodGet, ollamaBaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := streamHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding ollama models: %w", err)
	}

	out := make([]domain.APIModelInfo, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		out = append(out, domain.APIModelInfo{ID: m.Name, DisplayName: m.Name})
	}
	return out, nil
}

func (p *OllamaProvider) StreamMessage(
	_ string,
	modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	messages := buildOllamaMessages(history, system)
	toolDefs := toOllamaTools(tools)
	blocks, stopReason, usage, err := streamOllamaChat(modelID, messages, toolDefs, onDelta)
	if err != nil && len(toolDefs) > 0 && isOllamaToolsUnsupported(err) {
		// Model supports chat but not tools (e.g. some Gemma variants).
		// Retry without tools so the user still gets a response.
		return streamOllamaChat(modelID, messages, nil, onDelta)
	}
	return blocks, stopReason, usage, err
}

func streamOllamaChat(
	modelID string,
	messages []map[string]any,
	toolDefs []map[string]any,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	reqBody := struct {
		Model    string           `json:"model"`
		Messages []map[string]any `json:"messages"`
		Tools    []map[string]any `json:"tools,omitempty"`
		Stream   bool             `json:"stream"`
	}{
		Model:    modelID,
		Messages: messages,
		Tools:    toolDefs,
		Stream:   true,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, ollamaBaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := streamHTTPClient.Do(req)
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, "", Usage{}, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var text strings.Builder
	usage := Usage{}
	stopReason := "end_turn"
	type toolBuilder struct {
		id   string
		name string
		args map[string]any
	}
	toolBuilders := make(map[int]*toolBuilder)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk struct {
			Message *struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments any    `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Done           bool   `json:"done"`
			DoneReason     string `json:"done_reason"`
			PromptEvalCount int    `json:"prompt_eval_count"`
			EvalCount       int    `json:"eval_count"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Message != nil {
			if chunk.Message.Content != "" {
				text.WriteString(chunk.Message.Content)
				if onDelta != nil {
					onDelta(chunk.Message.Content)
				}
			}
			for idx, tc := range chunk.Message.ToolCalls {
				builder, ok := toolBuilders[idx]
				if !ok {
					builder = &toolBuilder{}
					toolBuilders[idx] = builder
				}
				if tc.ID != "" {
					builder.id = tc.ID
				}
				if tc.Function.Name != "" {
					builder.name = tc.Function.Name
				}
				builder.args = parseOllamaArgs(tc.Function.Arguments)
			}
		}
		if chunk.PromptEvalCount > 0 {
			usage.InputTokens = chunk.PromptEvalCount
		}
		if chunk.EvalCount > 0 {
			usage.OutputTokens = chunk.EvalCount
		}
		if chunk.Done && chunk.DoneReason != "" {
			stopReason = normalizeOllamaStop(chunk.DoneReason)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", usage, fmt.Errorf("reading stream: %w", err)
	}

	var blocks []domain.ContentBlock
	if text.String() != "" {
		blocks = append(blocks, domain.ContentBlock{Type: "text", Text: text.String()})
	}

	if len(toolBuilders) > 0 {
		keys := make([]int, 0, len(toolBuilders))
		for k := range toolBuilders {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, idx := range keys {
			b := toolBuilders[idx]
			if b.id == "" {
				b.id = fmt.Sprintf("ollama_tool_%d", idx+1)
			}
			if b.args == nil {
				b.args = map[string]any{}
			}
			blocks = append(blocks, domain.ContentBlock{
				Type:      "tool_use",
				ToolUseID: b.id,
				ToolName:  b.name,
				ToolInput: b.args,
			})
		}
		stopReason = "tool_use"
	}

	return blocks, stopReason, usage, nil
}

func isOllamaToolsUnsupported(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not support tools")
}

func buildOllamaMessages(history []domain.TranscriptMessage, system string) []map[string]any {
	msgs := make([]map[string]any, 0, len(history)+1)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": system,
		})
	}
	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.HasBlocks() {
			if m.Role == "assistant" {
				var textParts []string
				var toolCalls []map[string]any
				for _, blk := range m.Blocks {
					switch blk.Type {
					case "text":
						if strings.TrimSpace(blk.Text) != "" {
							textParts = append(textParts, blk.Text)
						}
					case "tool_use":
						toolCalls = append(toolCalls, map[string]any{
							"id":   blk.ToolUseID,
							"type": "function",
							"function": map[string]any{
								"name":      blk.ToolName,
								"arguments": blk.ToolInput,
							},
						})
					}
				}
				msg := map[string]any{
					"role":    "assistant",
					"content": strings.Join(textParts, "\n"),
				}
				if len(toolCalls) > 0 {
					msg["tool_calls"] = toolCalls
				}
				msgs = append(msgs, msg)
				continue
			}

			// User tool_result blocks are represented as tool messages.
			for _, blk := range m.Blocks {
				if blk.Type != "tool_result" {
					continue
				}
				msg := map[string]any{
					"role":    "tool",
					"content": blk.ToolResult,
				}
				if blk.ToolName != "" {
					msg["name"] = blk.ToolName
				}
				if blk.ToolUseID != "" {
					msg["tool_call_id"] = blk.ToolUseID
				}
				msgs = append(msgs, msg)
			}

			// Preserve any regular user text in block-form messages.
			var userTextParts []string
			for _, blk := range m.Blocks {
				switch blk.Type {
				case "text":
					if strings.TrimSpace(blk.Text) != "" {
						userTextParts = append(userTextParts, blk.Text)
					}
				}
			}
			if len(userTextParts) > 0 {
				msgs = append(msgs, map[string]any{
					"role":    "user",
					"content": strings.Join(userTextParts, "\n"),
				})
			}
			continue
		}

		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		msgs = append(msgs, map[string]any{
			"role":    m.Role,
			"content": content,
		})
	}
	return msgs
}

func toOllamaTools(specs []ToolSpec) []map[string]any {
	if len(specs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(specs))
	for _, s := range specs {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = convertOllamaProp(v)
		}
		req := s.Required
		if req == nil {
			req = []string{}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        s.Name,
				"description": s.Description,
				"parameters": map[string]any{
					"type":       "object",
					"properties": props,
					"required":   req,
				},
			},
		})
	}
	return out
}

func convertOllamaProp(v ToolProp) map[string]any {
	prop := map[string]any{"type": v.Type}
	if v.Description != "" {
		prop["description"] = v.Description
	}
	if len(v.Enum) > 0 {
		prop["enum"] = v.Enum
	}
	if v.Items != nil {
		prop["items"] = convertOllamaProp(*v.Items)
	}
	if len(v.Properties) > 0 {
		nested := make(map[string]any, len(v.Properties))
		for k, np := range v.Properties {
			nested[k] = convertOllamaProp(np)
		}
		prop["properties"] = nested
	}
	if len(v.Required) > 0 {
		prop["required"] = v.Required
	}
	return prop
}

func parseOllamaArgs(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	case string:
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) == nil {
			return m
		}
	}
	if v != nil {
		if b, err := json.Marshal(v); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				return m
			}
		}
	}
	return map[string]any{}
}

func normalizeOllamaStop(reason string) string {
	switch strings.TrimSpace(strings.ToLower(reason)) {
	case "", "stop":
		return "end_turn"
	default:
		return reason
	}
}
