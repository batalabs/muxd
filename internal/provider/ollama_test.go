package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
)

func TestOllamaProvider_StreamMessage_TextOnly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"Hello "},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"world"},"prompt_eval_count":12,"eval_count":7,"done":true,"done_reason":"stop"}`)
	}))
	defer ts.Close()

	prev := ollamaBaseURL
	SetOllamaBaseURL(ts.URL)
	t.Cleanup(func() { SetOllamaBaseURL(prev) })

	p := &OllamaProvider{}
	var deltas []string
	blocks, stop, usage, err := p.StreamMessage("", "gemma3:4b", nil, nil, "", func(s string) {
		deltas = append(deltas, s)
	})
	if err != nil {
		t.Fatalf("StreamMessage error: %v", err)
	}
	if stop != "end_turn" {
		t.Fatalf("stop = %q, want end_turn", stop)
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v, want input=12 output=7", usage)
	}
	if got := strings.Join(deltas, ""); got != "Hello world" {
		t.Fatalf("delta concat = %q", got)
	}
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "Hello world" {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestOllamaProvider_StreamMessage_ToolUse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"function":{"name":"list_files","arguments":{"path":"."}}}]}, "done":true}`)
	}))
	defer ts.Close()

	prev := ollamaBaseURL
	SetOllamaBaseURL(ts.URL)
	t.Cleanup(func() { SetOllamaBaseURL(prev) })

	p := &OllamaProvider{}
	toolSpecs := []ToolSpec{
		{
			Name:        "list_files",
			Description: "List files",
			Properties: map[string]ToolProp{
				"path": {Type: "string"},
			},
			Required: []string{"path"},
		},
	}
	blocks, stop, _, err := p.StreamMessage("", "gemma3:4b", nil, toolSpecs, "", nil)
	if err != nil {
		t.Fatalf("StreamMessage error: %v", err)
	}
	if stop != "tool_use" {
		t.Fatalf("stop = %q, want tool_use", stop)
	}
	if len(blocks) != 1 || blocks[0].Type != "tool_use" {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
	if blocks[0].ToolName != "list_files" {
		t.Fatalf("tool name = %q, want list_files", blocks[0].ToolName)
	}
	if blocks[0].ToolInput["path"] != "." {
		t.Fatalf("tool input path = %#v, want \".\"", blocks[0].ToolInput["path"])
	}
}

func TestBuildOllamaMessages_MapsToolHistory(t *testing.T) {
	history := []domain.TranscriptMessage{
		{
			Role: "assistant",
			Blocks: []domain.ContentBlock{
				{Type: "text", Text: "I'll list files."},
				{Type: "tool_use", ToolUseID: "t1", ToolName: "list_files", ToolInput: map[string]any{"path": "."}},
			},
		},
		{
			Role: "user",
			Blocks: []domain.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", ToolName: "list_files", ToolResult: "a.txt\nb.txt"},
			},
		},
	}
	msgs := buildOllamaMessages(history, "")
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if msgs[0]["role"] != "assistant" {
		t.Fatalf("first role = %#v, want assistant", msgs[0]["role"])
	}
	if msgs[1]["role"] != "tool" {
		t.Fatalf("second role = %#v, want tool", msgs[1]["role"])
	}
}

func TestOllamaProvider_StreamMessage_FallsBackWhenModelLacksToolSupport(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"registry.ollama.ai/library/gemma3:4b does not support tools"}`)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"Tool-free fallback works"},"done":true,"done_reason":"stop"}`)
	}))
	defer ts.Close()

	prev := ollamaBaseURL
	SetOllamaBaseURL(ts.URL)
	t.Cleanup(func() { SetOllamaBaseURL(prev) })

	p := &OllamaProvider{}
	toolSpecs := []ToolSpec{
		{
			Name:        "list_files",
			Description: "List files",
			Properties: map[string]ToolProp{
				"path": {Type: "string"},
			},
			Required: []string{"path"},
		},
	}
	blocks, stop, _, err := p.StreamMessage("", "gemma3:4b", nil, toolSpecs, "", nil)
	if err != nil {
		t.Fatalf("StreamMessage error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 requests (tools + fallback), got %d", callCount)
	}
	if stop != "end_turn" {
		t.Fatalf("stop = %q, want end_turn", stop)
	}
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
	if blocks[0].Text != "Tool-free fallback works" {
		t.Fatalf("text = %q", blocks[0].Text)
	}
}
