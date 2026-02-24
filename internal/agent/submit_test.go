package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

func TestService_Submit_toolUseParallel(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "test", Model: "fake"}
	store.addSession(sess)

	// First call returns tool_use, second call returns end_turn
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(callCount.Add(1))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if n == 1 {
			// Return tool_use for list_files (non-sequential tool → parallel path)
			writeSSE(w, "message_start", map[string]any{
				"message": map[string]any{"usage": map[string]any{"input_tokens": 10, "output_tokens": 0}},
			})
			writeSSE(w, "content_block_start", map[string]any{
				"index":         0,
				"content_block": map[string]any{"type": "tool_use", "id": "tu_1", "name": "list_files"},
			})
			writeSSE(w, "content_block_delta", map[string]any{
				"index": 0,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"."}`},
			})
			writeSSE(w, "content_block_stop", map[string]any{"index": 0})
			writeSSE(w, "message_delta", map[string]any{
				"usage": map[string]any{"output_tokens": 5},
				"delta": map[string]any{"stop_reason": "tool_use"},
			})
		} else {
			// Second call: return text + end_turn
			writeSSE(w, "message_start", map[string]any{
				"message": map[string]any{"usage": map[string]any{"input_tokens": 20, "output_tokens": 0}},
			})
			writeSSE(w, "content_block_start", map[string]any{
				"index":         0,
				"content_block": map[string]any{"type": "text", "id": "", "name": ""},
			})
			writeSSE(w, "content_block_delta", map[string]any{
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": "Here are the files."},
			})
			writeSSE(w, "content_block_stop", map[string]any{"index": 0})
			writeSSE(w, "message_delta", map[string]any{
				"usage": map[string]any{"output_tokens": 10},
				"delta": map[string]any{"stop_reason": "end_turn"},
			})
		}
	}))
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake", "fake", store, sess, nil)
	svc.Cwd = "."

	var events []Event
	var mu sync.Mutex
	svc.Submit("List files in the current directory", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	var gotToolStart, gotToolDone, gotTurnDone bool
	for _, evt := range events {
		switch evt.Kind {
		case EventToolStart:
			gotToolStart = true
			if evt.ToolName != "list_files" {
				t.Errorf("expected tool list_files, got %s", evt.ToolName)
			}
		case EventToolDone:
			gotToolDone = true
			if evt.ToolName != "list_files" {
				t.Errorf("expected tool list_files, got %s", evt.ToolName)
			}
		case EventTurnDone:
			gotTurnDone = true
		case EventError:
			t.Fatalf("unexpected error: %v", evt.Err)
		}
	}

	if !gotToolStart {
		t.Error("expected EventToolStart")
	}
	if !gotToolDone {
		t.Error("expected EventToolDone")
	}
	if !gotTurnDone {
		t.Error("expected EventTurnDone")
	}

	// API should have been called twice (tool_use -> end_turn)
	if int(callCount.Load()) != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount.Load())
	}
}

func TestService_Submit_toolUseSequential_askUser(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "test", Model: "fake"}
	store.addSession(sess)

	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(callCount.Add(1))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if n == 1 {
			// Return ask_user tool_use (sequential path)
			writeSSE(w, "message_start", map[string]any{
				"message": map[string]any{"usage": map[string]any{"input_tokens": 10, "output_tokens": 0}},
			})
			writeSSE(w, "content_block_start", map[string]any{
				"index":         0,
				"content_block": map[string]any{"type": "tool_use", "id": "tu_ask", "name": "ask_user"},
			})
			writeSSE(w, "content_block_delta", map[string]any{
				"index": 0,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"question":"What is the target?"}`},
			})
			writeSSE(w, "content_block_stop", map[string]any{"index": 0})
			writeSSE(w, "message_delta", map[string]any{
				"usage": map[string]any{"output_tokens": 5},
				"delta": map[string]any{"stop_reason": "tool_use"},
			})
		} else {
			// After receiving user answer, return end_turn
			writeSSE(w, "message_start", map[string]any{
				"message": map[string]any{"usage": map[string]any{"input_tokens": 20, "output_tokens": 0}},
			})
			writeSSE(w, "content_block_start", map[string]any{
				"index":         0,
				"content_block": map[string]any{"type": "text", "id": "", "name": ""},
			})
			writeSSE(w, "content_block_delta", map[string]any{
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": "Got it, thanks!"},
			})
			writeSSE(w, "content_block_stop", map[string]any{"index": 0})
			writeSSE(w, "message_delta", map[string]any{
				"usage": map[string]any{"output_tokens": 8},
				"delta": map[string]any{"stop_reason": "end_turn"},
			})
		}
	}))
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake", "fake", store, sess, nil)
	svc.Cwd = "/tmp"

	var events []Event
	var mu sync.Mutex
	svc.Submit("Help me configure", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()

		// Respond to ask_user
		if evt.Kind == EventAskUser {
			evt.AskResponse <- "production"
		}
	})

	mu.Lock()
	defer mu.Unlock()

	var gotAskUser, gotToolDone, gotTurnDone bool
	for _, evt := range events {
		switch evt.Kind {
		case EventAskUser:
			gotAskUser = true
			if evt.AskPrompt != "What is the target?" {
				t.Errorf("expected 'What is the target?', got %q", evt.AskPrompt)
			}
		case EventToolDone:
			gotToolDone = true
			if evt.ToolName == "ask_user" && evt.ToolResult != "production" {
				t.Errorf("expected ask_user result='production', got %q", evt.ToolResult)
			}
		case EventTurnDone:
			gotTurnDone = true
		case EventError:
			t.Fatalf("unexpected error: %v", evt.Err)
		}
	}

	if !gotAskUser {
		t.Error("expected EventAskUser")
	}
	if !gotToolDone {
		t.Error("expected EventToolDone for ask_user")
	}
	if !gotTurnDone {
		t.Error("expected EventTurnDone")
	}
}

func TestService_Submit_loopLimit(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "test", Model: "fake"}
	store.addSession(sess)

	// Server always returns tool_use to force infinite loop
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		writeSSE(w, "message_start", map[string]any{
			"message": map[string]any{"usage": map[string]any{"input_tokens": 10, "output_tokens": 0}},
		})
		writeSSE(w, "content_block_start", map[string]any{
			"index":         0,
			"content_block": map[string]any{"type": "tool_use", "id": "tu_loop", "name": "list_files"},
		})
		writeSSE(w, "content_block_delta", map[string]any{
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"."}`},
		})
		writeSSE(w, "content_block_stop", map[string]any{"index": 0})
		writeSSE(w, "message_delta", map[string]any{
			"usage": map[string]any{"output_tokens": 5},
			"delta": map[string]any{"stop_reason": "tool_use"},
		})
	}))
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake", "fake", store, sess, nil)
	svc.Cwd = "."

	var events []Event
	var mu sync.Mutex
	svc.Submit("Loop forever", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	var gotLimitError bool
	for _, evt := range events {
		if evt.Kind == EventError && strings.Contains(evt.Err.Error(), "loop limit exceeded") {
			gotLimitError = true
		}
	}
	if !gotLimitError {
		t.Error("expected loop limit exceeded error")
	}
}

func TestService_Submit_persistsErrorMessage(t *testing.T) {
	st := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "test", Model: "fake"}
	st.addSession(sess)

	// Return a non-retryable error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "bad model",
			},
		})
	}))
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake", "fake", st, sess, nil)
	svc.Cwd = "/tmp"

	svc.Submit("Hello", func(evt Event) {})

	// Verify error message was persisted
	msgs, _ := st.GetMessages(sess.ID)
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages (user+error), got %d", len(msgs))
	}
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.Role != "assistant" {
		t.Errorf("expected last message role=assistant, got %s", lastMsg.Role)
	}
	if !strings.HasPrefix(lastMsg.Content, "Error:") {
		t.Errorf("expected error message, got %q", lastMsg.Content)
	}
}

func TestService_Submit_autoTitle(t *testing.T) {
	st := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "New Session", Model: "fake"}
	st.addSession(sess)

	server := fakeSSEServer(t, []domain.ContentBlock{
		{Type: "text", Text: "Response text"},
	}, "end_turn", 100, 50)
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake", "fake", st, sess, nil)
	svc.Cwd = "/tmp"

	var gotTitled bool
	var mu sync.Mutex
	svc.Submit("My first question", func(evt Event) {
		mu.Lock()
		defer mu.Unlock()
		if evt.Kind == EventTitled {
			gotTitled = true
		}
	})

	mu.Lock()
	defer mu.Unlock()

	if !gotTitled {
		t.Error("expected EventTitled on first response")
	}
	if sess.Title == "New Session" || sess.Title == "" {
		t.Errorf("expected title to be updated, got %q", sess.Title)
	}
}

// ---------------------------------------------------------------------------
// SSE helper
// ---------------------------------------------------------------------------

func writeSSE(w http.ResponseWriter, eventType string, payload map[string]any) {
	payload["type"] = eventType
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
