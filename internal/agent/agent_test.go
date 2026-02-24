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

// ---------------------------------------------------------------------------
// Mock store for tests
// ---------------------------------------------------------------------------

type mockStore struct {
	mu       sync.Mutex
	messages map[string][]domain.TranscriptMessage
	sessions map[string]*domain.Session
}

func newMockStore() *mockStore {
	return &mockStore{
		messages: make(map[string][]domain.TranscriptMessage),
		sessions: make(map[string]*domain.Session),
	}
}

func (s *mockStore) AppendMessage(sessionID, role, content string, tokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], domain.TranscriptMessage{
		Role:    role,
		Content: content,
	})
	return nil
}

func (s *mockStore) AppendMessageBlocks(sessionID, role string, blocks []domain.ContentBlock, tokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := domain.TranscriptMessage{Role: role, Blocks: blocks}
	msg.Content = msg.TextContent()
	s.messages[sessionID] = append(s.messages[sessionID], msg)
	return nil
}

func (s *mockStore) UpdateSessionTokens(sessionID string, inputTokens, outputTokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.InputTokens = inputTokens
		sess.OutputTokens = outputTokens
		sess.TotalTokens = inputTokens + outputTokens
	}
	return nil
}

func (s *mockStore) UpdateSessionTitle(sessionID, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Title = title
	}
	return nil
}

func (s *mockStore) UpdateSessionModel(sessionID, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Model = model
	}
	return nil
}

func (s *mockStore) GetMessages(sessionID string) ([]domain.TranscriptMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.messages[sessionID]
	out := make([]domain.TranscriptMessage, len(msgs))
	copy(out, msgs)
	return out, nil
}

func (s *mockStore) CreateSession(projectPath, model string) (*domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := &domain.Session{
		ID:          domain.NewUUID(),
		ProjectPath: projectPath,
		Title:       "New Session",
		Model:       model,
	}
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *mockStore) UpdateSessionTags(sessionID, tags string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Tags = tags
	}
	return nil
}

func (s *mockStore) SaveCompaction(sessionID, summaryText string, cutoffSequence int) error {
	return nil
}

func (s *mockStore) LatestCompaction(sessionID string) (string, int, error) {
	return "", 0, fmt.Errorf("no compaction")
}

func (s *mockStore) GetMessagesAfterSequence(sessionID string, afterSequence int) ([]domain.TranscriptMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[sessionID], nil
}

func (s *mockStore) MessageMaxSequence(sessionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages[sessionID]), nil
}

func (s *mockStore) BranchSession(fromSessionID string, atSequence int) (*domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.sessions[fromSessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	newSess := &domain.Session{
		ID:              domain.NewUUID(),
		ProjectPath:     src.ProjectPath,
		Title:           src.Title + " (branch)",
		Model:           src.Model,
		ParentSessionID: fromSessionID,
		BranchPoint:     atSequence,
	}
	s.sessions[newSess.ID] = newSess
	return newSess, nil
}

func (s *mockStore) addSession(sess *domain.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

// ---------------------------------------------------------------------------
// SSE event type for fake server
// ---------------------------------------------------------------------------

type sseMessage struct {
	Type    string `json:"type"`
	Message *struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message,omitempty"`
}

// fakeSSEServer creates an httptest.Server that returns a canned SSE response
// simulating an Anthropic messages stream.
func fakeSSEServer(t *testing.T, blocks []domain.ContentBlock, stopReason string, inputTokens, outputTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// message_start
		msgStart := sseMessage{Type: "message_start"}
		msgStart.Message = &struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}{}
		msgStart.Message.Usage.InputTokens = inputTokens
		data, _ := json.Marshal(msgStart)
		fmt.Fprintf(w, "data: %s\n\n", data)

		for i, b := range blocks {
			// content_block_start
			startEvt := map[string]any{
				"type":  "content_block_start",
				"index": i,
				"content_block": map[string]any{
					"type": b.Type,
					"id":   b.ToolUseID,
					"name": b.ToolName,
				},
			}
			data, _ = json.Marshal(startEvt)
			fmt.Fprintf(w, "data: %s\n\n", data)

			// content_block_delta
			if b.Type == "text" {
				deltaEvt := map[string]any{
					"type":  "content_block_delta",
					"index": i,
					"delta": map[string]any{
						"type": "text_delta",
						"text": b.Text,
					},
				}
				data, _ = json.Marshal(deltaEvt)
				fmt.Fprintf(w, "data: %s\n\n", data)
			} else if b.Type == "tool_use" {
				inputJSON, _ := json.Marshal(b.ToolInput)
				deltaEvt := map[string]any{
					"type":  "content_block_delta",
					"index": i,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": string(inputJSON),
					},
				}
				data, _ = json.Marshal(deltaEvt)
				fmt.Fprintf(w, "data: %s\n\n", data)
			}

			// content_block_stop
			stopEvt := map[string]any{
				"type":  "content_block_stop",
				"index": i,
			}
			data, _ = json.Marshal(stopEvt)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}

		// message_delta
		msgDelta := map[string]any{
			"type": "message_delta",
			"usage": map[string]any{
				"output_tokens": outputTokens,
			},
			"delta": map[string]any{
				"stop_reason": stopReason,
			},
		}
		data, _ = json.Marshal(msgDelta)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}))
}

func TestExecuteToolCall_unknownTool(t *testing.T) {
	call := domain.ContentBlock{
		Type:      "tool_use",
		ToolUseID: "tu_1",
		ToolName:  "nonexistent_tool",
		ToolInput: map[string]any{},
	}
	result, isError := ExecuteToolCall(call, nil)
	if !isError {
		t.Fatal("expected isError=true for unknown tool")
	}
	if !strings.Contains(result, "Unknown tool") {
		t.Fatalf("expected 'Unknown tool' in result, got: %s", result)
	}
}

func TestExecuteToolCall_fileReadMissing(t *testing.T) {
	call := domain.ContentBlock{
		Type:      "tool_use",
		ToolUseID: "tu_2",
		ToolName:  "file_read",
		ToolInput: map[string]any{"path": "/nonexistent/path/to/file.txt"},
	}
	result, isError := ExecuteToolCall(call, nil)
	if !isError {
		t.Fatal("expected isError=true for missing file")
	}
	if result == "" {
		t.Fatal("expected non-empty error result")
	}
}

func TestExecuteToolCall_listFiles(t *testing.T) {
	call := domain.ContentBlock{
		Type:      "tool_use",
		ToolUseID: "tu_3",
		ToolName:  "list_files",
		ToolInput: map[string]any{"path": "."},
	}
	result, isError := ExecuteToolCall(call, nil)
	if isError {
		t.Fatalf("unexpected error: %s", result)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestStreamMessagePure_textResponse(t *testing.T) {
	server := fakeSSEServer(t, []domain.ContentBlock{
		{Type: "text", Text: "Hello, world!"},
	}, "end_turn", 100, 50)
	defer server.Close()

	var deltas []string
	blocks, stopReason, usage, err := provider.StreamMessagePureWithURL(
		server.URL, "fake-key", "fake-model",
		[]domain.TranscriptMessage{{Role: "user", Content: "Hi"}},
		nil,
		"system prompt",
		func(delta string) { deltas = append(deltas, delta) },
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got %s", stopReason)
	}
	if usage.InputTokens != 100 {
		t.Fatalf("expected 100 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Fatalf("expected 50 output tokens, got %d", usage.OutputTokens)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text != "Hello, world!" {
		t.Fatalf("expected 'Hello, world!', got %q", blocks[0].Text)
	}
	if len(deltas) != 1 || deltas[0] != "Hello, world!" {
		t.Fatalf("expected one delta 'Hello, world!', got %v", deltas)
	}
}

func TestStreamMessagePure_errorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "bad request",
			},
		})
	}))
	defer server.Close()

	_, _, _, err := provider.StreamMessagePureWithURL(
		server.URL, "fake-key", "fake-model",
		[]domain.TranscriptMessage{{Role: "user", Content: "Hi"}},
		nil, "system prompt", nil,
	)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "invalid_request_error") {
		t.Fatalf("expected error to contain 'invalid_request_error', got: %v", err)
	}
}

func TestService_Submit_endTurn(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{
		ID:    domain.NewUUID(),
		Title: "New Session",
		Model: "fake-model",
	}
	store.addSession(sess)

	// Create a fake server that returns a text response
	server := fakeSSEServer(t, []domain.ContentBlock{
		{Type: "text", Text: "I can help with that."},
	}, "end_turn", 100, 50)
	defer server.Close()

	// Override the API URL for testing
	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake-model", "fake", store, sess, nil)

	var events []Event
	var mu sync.Mutex
	svc.Submit("Hello", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	// Should have: delta(s), streamDone, turnDone
	var gotDelta, gotStreamDone, gotTurnDone bool
	for _, evt := range events {
		switch evt.Kind {
		case EventDelta:
			gotDelta = true
		case EventStreamDone:
			gotStreamDone = true
			if evt.StopReason != "end_turn" {
				t.Errorf("expected stop_reason=end_turn, got %s", evt.StopReason)
			}
		case EventTurnDone:
			gotTurnDone = true
		case EventError:
			t.Fatalf("unexpected error event: %v", evt.Err)
		}
	}

	if !gotDelta {
		t.Error("expected at least one delta event")
	}
	if !gotStreamDone {
		t.Error("expected streamDone event")
	}
	if !gotTurnDone {
		t.Error("expected turnDone event")
	}

	// Verify messages were persisted
	msgs, err := store.GetMessages(sess.ID)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 persisted messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
		t.Errorf("first message should be user 'Hello', got %s %q", msgs[0].Role, msgs[0].Content)
	}
}

func TestService_Cancel(t *testing.T) {
	svc := &Service{
		apiKey:  "fake",
		modelID: "fake",
	}
	svc.Cancel()
	if !svc.cancelled {
		t.Error("expected cancelled=true after Cancel()")
	}
}

func TestService_IsRunning(t *testing.T) {
	svc := &Service{}
	if svc.IsRunning() {
		t.Error("expected IsRunning=false initially")
	}
}

func TestService_SetModel(t *testing.T) {
	svc := &Service{
		apiKey:  "fake",
		modelID: "old-model",
	}
	svc.SetModel("new-label", "new-model-id")
	if svc.modelID != "new-model-id" {
		t.Errorf("expected modelID=new-model-id, got %s", svc.modelID)
	}
	if svc.modelLabel != "new-label" {
		t.Errorf("expected modelLabel=new-label, got %s", svc.modelLabel)
	}
}

func TestService_Messages(t *testing.T) {
	svc := &Service{
		messages: []domain.TranscriptMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
	}
	msgs := svc.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Verify it's a copy
	msgs[0].Content = "modified"
	if svc.messages[0].Content == "modified" {
		t.Error("Messages() should return a copy, not a reference")
	}
}

func TestService_NewSession(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{
		ID:    domain.NewUUID(),
		Title: "New Session",
		Model: "model",
	}
	store.addSession(sess)
	svc := NewService("key", "model", "label", store, sess, nil)
	svc.messages = []domain.TranscriptMessage{{Role: "user", Content: "old"}}
	svc.inputTokens = 100

	if err := svc.NewSession("/tmp/new"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if svc.session.ID == sess.ID {
		t.Error("expected new session ID")
	}
	if len(svc.messages) != 0 {
		t.Error("expected empty messages after NewSession")
	}
	if svc.inputTokens != 0 {
		t.Error("expected inputTokens=0 after NewSession")
	}
}

func TestCompactMessages(t *testing.T) {
	t.Run("does not compact short conversations", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		}
		result := CompactMessages(msgs)
		if result.DidCompact {
			t.Error("expected DidCompact=false for short conversation")
		}
		if len(result.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(result.Messages))
		}
		if result.Dropped != nil {
			t.Errorf("expected nil Dropped, got %d messages", len(result.Dropped))
		}
	})

	t.Run("compacts long conversations", func(t *testing.T) {
		// Create a conversation with more than CompactKeepTail+2 messages
		var msgs []domain.TranscriptMessage
		msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: "first question"})
		msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: "first answer"})
		for i := 0; i < 30; i++ {
			if i%2 == 0 {
				msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: fmt.Sprintf("q%d", i)})
			} else {
				msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)})
			}
		}

		result := CompactMessages(msgs)
		if !result.DidCompact {
			t.Error("expected DidCompact=true for long conversation")
		}
		if len(result.Messages) >= len(msgs) {
			t.Errorf("expected compacted length < %d, got %d", len(msgs), len(result.Messages))
		}
		// Should start with the first user message
		if result.Messages[0].Role != "user" || result.Messages[0].Content != "first question" {
			t.Errorf("expected first message preserved, got %s %q", result.Messages[0].Role, result.Messages[0].Content)
		}
		// Should contain compaction notice
		found := false
		for _, m := range result.Messages {
			if strings.Contains(m.Content, "compacted") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected compaction notice in result")
		}
		// Dropped messages should be non-empty
		if len(result.Dropped) == 0 {
			t.Error("expected non-empty Dropped slice")
		}
		// Verify dropped messages don't include head or tail
		if result.Dropped[0].Content == "first question" {
			t.Error("Dropped should not include the head message")
		}
	})
}

func TestSerializeMessagesForSummary(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		result := serializeMessagesForSummary(nil)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("text only messages", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		}
		result := serializeMessagesForSummary(msgs)
		if !strings.Contains(result, "[user]: hello") {
			t.Errorf("expected user message, got %q", result)
		}
		if !strings.Contains(result, "[assistant]: world") {
			t.Errorf("expected assistant message, got %q", result)
		}
	})

	t.Run("messages with tool blocks", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{
				Role: "assistant",
				Blocks: []domain.ContentBlock{
					{Type: "text", Text: "Let me read the file."},
					{Type: "tool_use", ToolName: "file_read", ToolInput: map[string]any{"path": "/tmp/test.go"}},
				},
			},
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolName: "file_read", ToolResult: "package main\nfunc main() {}"},
				},
			},
		}
		result := serializeMessagesForSummary(msgs)
		if !strings.Contains(result, "[tool: file_read]") {
			t.Errorf("expected tool_use block, got %q", result)
		}
		if !strings.Contains(result, "[result: file_read]") {
			t.Errorf("expected tool_result block, got %q", result)
		}
	})

	t.Run("truncates long output", func(t *testing.T) {
		// Create messages that will exceed 30k chars
		var msgs []domain.TranscriptMessage
		longContent := strings.Repeat("x", 20000)
		for i := 0; i < 5; i++ {
			msgs = append(msgs, domain.TranscriptMessage{
				Role:    "user",
				Content: fmt.Sprintf("message %d: %s", i, longContent),
			})
		}
		result := serializeMessagesForSummary(msgs)
		if len(result) > 35000 { // allow some overhead for the truncation marker
			t.Errorf("expected truncated output, got length %d", len(result))
		}
		if !strings.Contains(result, "...[truncated]...") {
			t.Error("expected truncation marker")
		}
	})

	t.Run("truncates long tool results", func(t *testing.T) {
		longResult := strings.Repeat("x", 500)
		msgs := []domain.TranscriptMessage{
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolName: "bash", ToolResult: longResult},
				},
			},
		}
		result := serializeMessagesForSummary(msgs)
		// The tool result should be truncated to 200 chars + "..."
		if strings.Contains(result, longResult) {
			t.Error("expected tool result to be truncated")
		}
	})
}

func TestGenerateCompactionSummary(t *testing.T) {
	t.Run("returns LLM summary on success", func(t *testing.T) {
		server := fakeSSEServer(t, []domain.ContentBlock{
			{Type: "text", Text: "## Topics discussed\n- File editing\n\n## Files modified\n- main.go\n\n## Tools used\n- file_read\n\n## Key decisions\n- Use Go\n\n## Current task state\nIn progress"},
		}, "end_turn", 100, 50)
		defer server.Close()

		origURL := provider.TestAPIURL
		provider.TestAPIURL = server.URL
		defer func() { provider.TestAPIURL = origURL }()

		svc := &Service{apiKey: "fake", modelID: "fake"}
		dropped := []domain.TranscriptMessage{
			{Role: "user", Content: "edit main.go"},
			{Role: "assistant", Content: "I'll edit it now."},
		}

		summary := svc.generateCompactionSummary(dropped)
		if !strings.Contains(summary, "[Conversation summary]") {
			t.Errorf("expected summary prefix, got %q", summary)
		}
		if !strings.Contains(summary, "Topics discussed") {
			t.Errorf("expected structured summary content, got %q", summary)
		}
	})

	t.Run("returns fallback on error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"type":    "api_error",
					"message": "internal error",
				},
			})
		}))
		defer server.Close()

		origURL := provider.TestAPIURL
		provider.TestAPIURL = server.URL
		defer func() { provider.TestAPIURL = origURL }()

		svc := &Service{apiKey: "fake", modelID: "fake"}
		dropped := []domain.TranscriptMessage{
			{Role: "user", Content: "hello"},
		}

		summary := svc.generateCompactionSummary(dropped)
		if !strings.Contains(summary, "No summary available") {
			t.Errorf("expected fallback message, got %q", summary)
		}
	})

	t.Run("returns fallback for empty dropped", func(t *testing.T) {
		svc := &Service{apiKey: "fake", modelID: "fake"}
		summary := svc.generateCompactionSummary(nil)
		if !strings.Contains(summary, "No summary available") {
			t.Errorf("expected fallback message, got %q", summary)
		}
	})
}

func TestCompactIfNeeded(t *testing.T) {
	t.Run("no compaction below threshold", func(t *testing.T) {
		svc := &Service{
			apiKey:          "fake",
			modelID:         "fake",
			lastInputTokens: 1000, // well below threshold
			messages: []domain.TranscriptMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
		}

		var events []Event
		svc.compactIfNeeded(func(evt Event) {
			events = append(events, evt)
		})

		if len(events) != 0 {
			t.Errorf("expected no events, got %d", len(events))
		}
		if len(svc.messages) != 2 {
			t.Errorf("expected messages unchanged, got %d", len(svc.messages))
		}
	})

	t.Run("compacts and emits event above threshold", func(t *testing.T) {
		// Set up fake server for the summary call
		server := fakeSSEServer(t, []domain.ContentBlock{
			{Type: "text", Text: "## Topics\n- Greeting exchange"},
		}, "end_turn", 10, 5)
		defer server.Close()

		origURL := provider.TestAPIURL
		provider.TestAPIURL = server.URL
		defer func() { provider.TestAPIURL = origURL }()

		// Build a long conversation
		var msgs []domain.TranscriptMessage
		msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: "first question"})
		msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: "first answer"})
		for i := 0; i < 30; i++ {
			if i%2 == 0 {
				msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: fmt.Sprintf("q%d", i)})
			} else {
				msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)})
			}
		}

		svc := &Service{
			apiKey:          "fake",
			modelID:         "fake",
			lastInputTokens: CompactThreshold + 1,
			messages:        msgs,
		}

		var events []Event
		svc.compactIfNeeded(func(evt Event) {
			events = append(events, evt)
		})

		// Should have emitted EventCompacted
		var gotCompacted bool
		for _, evt := range events {
			if evt.Kind == EventCompacted {
				gotCompacted = true
			}
		}
		if !gotCompacted {
			t.Error("expected EventCompacted event")
		}

		// Messages should be compacted
		if len(svc.messages) >= len(msgs) {
			t.Errorf("expected fewer messages after compaction, got %d (was %d)", len(svc.messages), len(msgs))
		}

		// lastInputTokens should be reset
		if svc.lastInputTokens != 0 {
			t.Errorf("expected lastInputTokens=0, got %d", svc.lastInputTokens)
		}

		// The placeholder should have been replaced with LLM summary
		foundSummary := false
		for _, m := range svc.messages {
			if strings.Contains(m.Content, "[Conversation summary]") {
				foundSummary = true
				break
			}
		}
		if !foundSummary {
			t.Error("expected LLM summary to replace placeholder in messages")
		}
	})
}

// ---------------------------------------------------------------------------
// Retry tests
// ---------------------------------------------------------------------------

// fakeRateLimitThenSuccessServer returns 429 for the first failCount requests
// with retry-after-ms header, then returns a successful SSE response.
func fakeRateLimitThenSuccessServer(t *testing.T, failCount int, retryAfterMs string) *httptest.Server {
	t.Helper()
	var callCount atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(callCount.Add(1))
		if n <= failCount {
			w.Header().Set("Retry-After-Ms", retryAfterMs)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"type":    "rate_limit_error",
					"message": "rate limit exceeded",
				},
			})
			return
		}

		// Success response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"usage": map[string]any{"input_tokens": 10, "output_tokens": 0},
			},
		}
		data, _ := json.Marshal(msgStart)
		fmt.Fprintf(w, "data: %s\n\n", data)

		blockStart := map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "id": "", "name": ""},
		}
		data, _ = json.Marshal(blockStart)
		fmt.Fprintf(w, "data: %s\n\n", data)

		delta := map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "Success after retry"},
		}
		data, _ = json.Marshal(delta)
		fmt.Fprintf(w, "data: %s\n\n", data)

		msgDelta := map[string]any{
			"type":  "message_delta",
			"usage": map[string]any{"output_tokens": 5},
			"delta": map[string]any{"stop_reason": "end_turn"},
		}
		data, _ = json.Marshal(msgDelta)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}))
}

// fakeAlways429Server returns 429 for every request.
func fakeAlways429Server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "50")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "rate_limit_error",
				"message": "rate limit exceeded",
			},
		})
	}))
}

func TestService_Submit_retryOnRateLimit(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "New Session", Model: "fake-model"}
	store.addSession(sess)

	// Server returns 429 once with retry-after-ms: 100, then succeeds
	server := fakeRateLimitThenSuccessServer(t, 1, "100")
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake-model", "fake", store, sess, nil)

	var events []Event
	var mu sync.Mutex
	svc.Submit("Hello", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	var gotRetrying, gotDelta, gotTurnDone bool
	for _, evt := range events {
		switch evt.Kind {
		case EventRetrying:
			gotRetrying = true
			if evt.RetryAttempt != 1 {
				t.Errorf("expected RetryAttempt=1, got %d", evt.RetryAttempt)
			}
		case EventDelta:
			gotDelta = true
		case EventTurnDone:
			gotTurnDone = true
		case EventError:
			t.Fatalf("unexpected error event: %v", evt.Err)
		}
	}

	if !gotRetrying {
		t.Error("expected EventRetrying event")
	}
	if !gotDelta {
		t.Error("expected delta event after retry")
	}
	if !gotTurnDone {
		t.Error("expected turnDone event after retry")
	}
}

func TestService_Submit_nonRetryableError(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "New Session", Model: "fake-model"}
	store.addSession(sess)

	// Server returns 400 (not retryable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "bad request",
			},
		})
	}))
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake-model", "fake", store, sess, nil)

	var events []Event
	var mu sync.Mutex
	svc.Submit("Hello", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	var gotRetrying, gotError bool
	for _, evt := range events {
		switch evt.Kind {
		case EventRetrying:
			gotRetrying = true
		case EventError:
			gotError = true
			if !strings.Contains(evt.Err.Error(), "invalid_request_error") {
				t.Errorf("expected invalid_request_error in error, got: %v", evt.Err)
			}
		}
	}

	if gotRetrying {
		t.Error("should not retry non-retryable errors")
	}
	if !gotError {
		t.Error("expected error event for non-retryable error")
	}
}

func TestService_Submit_retryExhausted(t *testing.T) {
	store := newMockStore()
	sess := &domain.Session{ID: domain.NewUUID(), Title: "New Session", Model: "fake-model"}
	store.addSession(sess)

	// Server always returns 429
	server := fakeAlways429Server(t)
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := NewService("fake-key", "fake-model", "fake", store, sess, nil)

	var events []Event
	var mu sync.Mutex
	svc.Submit("Hello", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	retryCount := 0
	var gotError bool
	for _, evt := range events {
		switch evt.Kind {
		case EventRetrying:
			retryCount++
		case EventError:
			gotError = true
			if !strings.Contains(evt.Err.Error(), "rate_limit_error") {
				t.Errorf("expected rate_limit_error in final error, got: %v", evt.Err)
			}
		}
	}

	if retryCount != maxRetries {
		t.Errorf("expected %d retry events, got %d", maxRetries, retryCount)
	}
	if !gotError {
		t.Error("expected error event after exhausting retries")
	}
}

func TestRepairDanglingToolUseMessages(t *testing.T) {
	t.Run("drops unmatched tool_use and partial tool_results", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "run audit"},
			{
				Role: "assistant",
				Blocks: []domain.ContentBlock{
					{Type: "tool_use", ToolUseID: "u1", ToolName: "file_read"},
					{Type: "tool_use", ToolUseID: "u2", ToolName: "file_read"},
				},
			},
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "file_read", ToolResult: "ok"},
				},
			},
			{Role: "assistant", Content: "Error: cancelled"},
		}

		got, changed := repairDanglingToolUseMessages(msgs)
		if !changed {
			t.Fatal("expected changed=true")
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 messages after repair, got %d", len(got))
		}
		if got[0].Role != "user" || got[1].Role != "assistant" {
			t.Fatalf("unexpected repaired sequence: %+v", got)
		}
	})

	t.Run("keeps valid adjacent tool_use/tool_result pair", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "run"},
			{
				Role: "assistant",
				Blocks: []domain.ContentBlock{
					{Type: "tool_use", ToolUseID: "u1", ToolName: "list_files"},
				},
			},
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "list_files", ToolResult: "ok"},
				},
			},
		}

		got, changed := repairDanglingToolUseMessages(msgs)
		if changed {
			t.Fatal("expected changed=false")
		}
		if len(got) != len(msgs) {
			t.Fatalf("expected %d messages, got %d", len(msgs), len(got))
		}
	})
}
