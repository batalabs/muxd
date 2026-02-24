package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
)

func TestParseOpenAISSE_textOnly(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}

data: {"choices":[{"index":0,"delta":{"content":" world"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}

data: [DONE]

`
	var deltas []string
	blocks, stop, usage, err := parseOpenAISSE(strings.NewReader(sse), func(s string) {
		deltas = append(deltas, s)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stop != "end_turn" {
		t.Errorf("stop = %q, want end_turn", stop)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	if len(blocks) != 1 || blocks[0].Text != "Hello world" {
		t.Errorf("blocks = %+v", blocks)
	}
	if len(deltas) != 2 || deltas[0] != "Hello" || deltas[1] != " world" {
		t.Errorf("deltas = %v", deltas)
	}
}

func TestParseOpenAISSE_toolCalls(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":""}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	blocks, stop, _, err := parseOpenAISSE(strings.NewReader(sse), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stop != "tool_use" {
		t.Errorf("stop = %q, want tool_use", stop)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 tool block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Type != "tool_use" {
		t.Errorf("type = %q", b.Type)
	}
	if b.ToolUseID != "call_1" {
		t.Errorf("tool_use_id = %q", b.ToolUseID)
	}
	if b.ToolName != "bash" {
		t.Errorf("tool_name = %q", b.ToolName)
	}
	if b.ToolInput["cmd"] != "ls" {
		t.Errorf("tool_input = %v", b.ToolInput)
	}
}

func TestParseOpenAISSE_textAndTools(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"content":"thinking..."}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"grep","arguments":"{\"q\":\"foo\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	blocks, _, _, err := parseOpenAISSE(strings.NewReader(sse), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + tool), got %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "thinking..." {
		t.Errorf("text block = %+v", blocks[0])
	}
	if blocks[1].Type != "tool_use" || blocks[1].ToolName != "grep" {
		t.Errorf("tool block = %+v", blocks[1])
	}
}

func TestParseOpenAISSE_invalidJSON(t *testing.T) {
	sse := `data: {invalid json}

data: {"choices":[{"index":0,"delta":{"content":"ok"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	blocks, _, _, err := parseOpenAISSE(strings.NewReader(sse), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid JSON line is skipped; valid content still parsed
	if len(blocks) != 1 || blocks[0].Text != "ok" {
		t.Errorf("blocks = %+v", blocks)
	}
}

func TestParseOpenAISSE_emptyStream(t *testing.T) {
	blocks, stop, _, err := parseOpenAISSE(strings.NewReader("data: [DONE]\n\n"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
	if stop != "end_turn" {
		t.Errorf("stop = %q, want end_turn", stop)
	}
}


func TestBuildOpenAIMessages(t *testing.T) {
	t.Run("system message", func(t *testing.T) {
		msgs := buildOpenAIMessages(nil, "You are helpful.")
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Role != "system" {
			t.Errorf("role = %q", msgs[0].Role)
		}
	})

	t.Run("user and assistant", func(t *testing.T) {
		history := []domain.TranscriptMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		}
		msgs := buildOpenAIMessages(history, "sys")
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		if msgs[0].Role != "system" {
			t.Errorf("msgs[0].Role = %q", msgs[0].Role)
		}
		if msgs[1].Role != "user" {
			t.Errorf("msgs[1].Role = %q", msgs[1].Role)
		}
		if msgs[2].Role != "assistant" {
			t.Errorf("msgs[2].Role = %q", msgs[2].Role)
		}
	})

	t.Run("no system", func(t *testing.T) {
		history := []domain.TranscriptMessage{
			{Role: "user", Content: "hi"},
		}
		msgs := buildOpenAIMessages(history, "")
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
	})
}

func TestGrokProvider_Name(t *testing.T) {
	p := &GrokProvider{}
	if p.Name() != "grok" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestMistralProvider_Name(t *testing.T) {
	p := &MistralProvider{}
	if p.Name() != "mistral" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := &OpenAIProvider{}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestGrokProvider_FetchModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "grok-1", "display_name": "Grok 1"},
			},
		})
	}))
	defer ts.Close()

	// Grok uses a const for base URL — we test via the common httptest pattern
	// by testing FetchModels error path instead
	p := &GrokProvider{}
	_, err := p.FetchModels("")
	if err == nil {
		t.Error("expected error with empty API key on real endpoint")
	}
}

func TestMistralProvider_FetchModels_error(t *testing.T) {
	p := &MistralProvider{}
	_, err := p.FetchModels("")
	if err == nil {
		t.Error("expected error with empty API key on real endpoint")
	}
}

func TestGrokProvider_StreamMessage_error(t *testing.T) {
	p := &GrokProvider{}
	_, _, _, err := p.StreamMessage("", "grok-1", nil, nil, "", nil)
	if err == nil {
		t.Error("expected error with empty API key")
	}
}

func TestMistralProvider_StreamMessage_error(t *testing.T) {
	p := &MistralProvider{}
	_, _, _, err := p.StreamMessage("", "mistral-large", nil, nil, "", nil)
	if err == nil {
		t.Error("expected error with empty API key")
	}
}

func TestFireworksProvider_StreamMessage_httpError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Header().Set("retry-after-ms", "5000")
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer ts.Close()

	orig := fireworksAPIBaseURL
	setFireworksBaseURL(ts.URL)
	defer setFireworksBaseURL(orig)

	p := &FireworksProvider{}
	_, _, _, err := p.StreamMessage("key", "model", nil, nil, "", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
}

func TestZAIProvider_StreamMessage_httpError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":{"type":"server_error","message":"internal"}}`)
	}))
	defer ts.Close()

	orig := zaiAPIBaseURL
	setZAIBaseURL(ts.URL)
	defer setZAIBaseURL(orig)

	p := &ZAIProvider{}
	_, _, _, err := p.StreamMessage("key", "model", nil, nil, "", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
}

func TestFireworksProvider_StreamMessage_success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	orig := fireworksAPIBaseURL
	setFireworksBaseURL(ts.URL)
	defer setFireworksBaseURL(orig)

	p := &FireworksProvider{}
	blocks, stop, usage, err := p.StreamMessage("key", "model", nil, nil, "sys", func(s string) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stop != "end_turn" {
		t.Errorf("stop = %q", stop)
	}
	if len(blocks) != 1 || blocks[0].Text != "hi" {
		t.Errorf("blocks = %+v", blocks)
	}
	if usage.InputTokens != 5 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestZAIProvider_StreamMessage_success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	orig := zaiAPIBaseURL
	setZAIBaseURL(ts.URL)
	defer setZAIBaseURL(orig)

	p := &ZAIProvider{}
	blocks, _, _, err := p.StreamMessage("key", "model", nil, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Text != "hello" {
		t.Errorf("blocks = %+v", blocks)
	}
}


func TestConvertOpenAIProp(t *testing.T) {
	prop := ToolProp{
		Type:        "array",
		Description: "list of names",
		Items:       &ToolProp{Type: "string"},
	}
	m := convertOpenAIProp(prop)
	if m["type"] != "array" {
		t.Errorf("type = %v", m["type"])
	}
	if m["description"] != "list of names" {
		t.Errorf("description = %v", m["description"])
	}
}

