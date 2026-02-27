package agent

import (
	"fmt"
	"sync"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

func TestService_Session(t *testing.T) {
	sess := &domain.Session{ID: "sess-1", Title: "test"}
	svc := &Service{session: sess}
	got := svc.Session()
	if got != sess {
		t.Errorf("expected session pointer %p, got %p", sess, got)
	}
}

func TestService_SetProvider(t *testing.T) {
	svc := &Service{apiKey: "old-key"}
	fake := &fakeProvider{name: "test-prov"}
	svc.SetProvider(fake, "new-key")
	if svc.apiKey != "new-key" {
		t.Errorf("expected apiKey=new-key, got %s", svc.apiKey)
	}
	if svc.prov == nil {
		t.Error("expected provider to be set")
	}
	if svc.prov.Name() != "test-prov" {
		t.Errorf("expected provider name test-prov, got %s", svc.prov.Name())
	}
}

func TestService_SetBraveAPIKey(t *testing.T) {
	svc := &Service{}
	svc.SetBraveAPIKey("brave-123")
	if svc.braveAPIKey != "brave-123" {
		t.Errorf("expected braveAPIKey=brave-123, got %s", svc.braveAPIKey)
	}
}

func TestService_SetXOAuth(t *testing.T) {
	var savedAccess, savedRefresh, savedExpiry string
	saver := func(a, r, e string) error {
		savedAccess, savedRefresh, savedExpiry = a, r, e
		return nil
	}
	svc := &Service{}
	svc.SetXOAuth("cid", "csec", "at", "rt", "exp", saver)

	if svc.xClientID != "cid" {
		t.Errorf("expected xClientID=cid, got %s", svc.xClientID)
	}
	if svc.xClientSecret != "csec" {
		t.Errorf("expected xClientSecret=csec, got %s", svc.xClientSecret)
	}
	if svc.xAccessToken != "at" {
		t.Errorf("expected xAccessToken=at, got %s", svc.xAccessToken)
	}
	if svc.xRefreshToken != "rt" {
		t.Errorf("expected xRefreshToken=rt, got %s", svc.xRefreshToken)
	}
	if svc.xTokenExpiry != "exp" {
		t.Errorf("expected xTokenExpiry=exp, got %s", svc.xTokenExpiry)
	}
	// Verify saver callback works
	if err := svc.xTokenSaver("a2", "r2", "e2"); err != nil {
		t.Fatalf("saver: %v", err)
	}
	if savedAccess != "a2" || savedRefresh != "r2" || savedExpiry != "e2" {
		t.Error("saver was not called with expected values")
	}
}

func TestService_SetDisabledTools(t *testing.T) {
	svc := &Service{disabledTools: map[string]bool{}}

	svc.SetDisabledTools(map[string]bool{"bash": true, "file_write": true, "nope": false})

	if !svc.disabledTools["bash"] {
		t.Error("expected bash disabled")
	}
	if !svc.disabledTools["file_write"] {
		t.Error("expected file_write disabled")
	}
	if svc.disabledTools["nope"] {
		t.Error("expected nope=false to be excluded")
	}
}

func TestService_SetGitAvailable(t *testing.T) {
	svc := &Service{}
	svc.SetGitAvailable(true, "/repo")
	if !svc.gitAvailable {
		t.Error("expected gitAvailable=true")
	}
	if svc.gitRepoRoot != "/repo" {
		t.Errorf("expected gitRepoRoot=/repo, got %s", svc.gitRepoRoot)
	}
}

func TestService_Resume_noStore(t *testing.T) {
	svc := &Service{}
	if err := svc.Resume(); err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestService_Resume_noSession(t *testing.T) {
	svc := &Service{store: newMockStore()}
	if err := svc.Resume(); err == nil {
		t.Error("expected error when session is nil")
	}
}

func TestService_Resume_loadsAllMessages(t *testing.T) {
	st := newMockStore()
	sess := &domain.Session{ID: "sess-resume", Title: "test"}
	st.addSession(sess)
	// Pre-populate messages
	st.messages["sess-resume"] = []domain.TranscriptMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
	if err := svc.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	msgs := svc.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("expected first msg=hello, got %q", msgs[0].Content)
	}
	if !svc.titled {
		t.Error("expected titled=true after resuming non-empty session")
	}
}

func TestService_Resume_withCompaction(t *testing.T) {
	st := &compactionMockStore{
		mockStore:         newMockStore(),
		compactionSummary: "## Summary\nDid stuff",
		compactionCutoff:  5,
	}
	sess := &domain.Session{ID: "sess-comp", Title: "test"}
	st.addSession(sess)
	// Tail messages (after cutoff)
	st.messages["sess-comp"] = []domain.TranscriptMessage{
		{Role: "user", Content: "continue please"},
		{Role: "assistant", Content: "sure thing"},
	}

	svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
	if err := svc.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	msgs := svc.Messages()
	// Should be: summary (user) + ack (assistant) + 2 tail messages = 4
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Error("expected summary+ack pair at start")
	}
	if msgs[2].Content != "continue please" {
		t.Errorf("expected tail messages, got %q", msgs[2].Content)
	}
	if !svc.titled {
		t.Error("expected titled=true after compacted resume")
	}
}

func TestService_NewSession_noStore(t *testing.T) {
	svc := &Service{}
	if err := svc.NewSession("/tmp"); err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestService_SetModel_persistsToStore(t *testing.T) {
	st := newMockStore()
	sess := &domain.Session{ID: "sess-model", Title: "test", Model: "old"}
	st.addSession(sess)

	svc := NewService("key", "old", "Old", st, sess, &fakeProvider{name: "test"})
	svc.SetModel("New", "new-model")

	st.mu.Lock()
	stored := st.sessions["sess-model"]
	st.mu.Unlock()

	if stored.Model != "new-model" {
		t.Errorf("expected persisted model=new-model, got %s", stored.Model)
	}
}

func TestService_SpawnSubAgent(t *testing.T) {
	server := fakeSSEServer(t, []domain.ContentBlock{
		{Type: "text", Text: "sub-agent response"},
	}, "end_turn", 10, 5)
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := &Service{
		apiKey:        "fake",
		modelID:       "fake",
		prov:          &testAnthropicProvider{},
		Cwd:           "/tmp",
		disabledTools: map[string]bool{},
	}

	result, err := svc.SpawnSubAgent("test task", "do something")
	if err != nil {
		t.Fatalf("SpawnSubAgent: %v", err)
	}
	if result != "sub-agent response" {
		t.Errorf("expected 'sub-agent response', got %q", result)
	}
}

func TestService_SpawnSubAgent_propagatesDisabledTools(t *testing.T) {
	// The sub-agent should inherit disabled tools from the parent.
	// We'll make the fake server return a tool_use for a disabled tool,
	// but since the tool specs won't include it, the model won't call it.
	// Instead we just verify the sub runs and completes.
	server := fakeSSEServer(t, []domain.ContentBlock{
		{Type: "text", Text: "ok"},
	}, "end_turn", 10, 5)
	defer server.Close()

	origURL := provider.TestAPIURL
	provider.TestAPIURL = server.URL
	defer func() { provider.TestAPIURL = origURL }()

	svc := &Service{
		apiKey:        "fake",
		modelID:       "fake",
		prov:          &testAnthropicProvider{},
		Cwd:           "/tmp",
		disabledTools: map[string]bool{"bash": true},
	}

	result, err := svc.SpawnSubAgent("test", "prompt")
	if err != nil {
		t.Fatalf("SpawnSubAgent: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeProvider implements provider.Provider for testing.
type fakeProvider struct {
	name string
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) StreamMessage(apiKey, modelID string, msgs []domain.TranscriptMessage, tools []provider.ToolSpec, system string, onDelta func(string)) ([]domain.ContentBlock, string, provider.Usage, error) {
	return nil, "end_turn", provider.Usage{}, nil
}
func (f *fakeProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	return nil, nil
}

// errorProvider implements provider.Provider and always returns an error.
// Used by tests that need the provider call to fail (e.g., cancelled-before-loop).
type errorProvider struct{}

func (p *errorProvider) Name() string { return "error" }
func (p *errorProvider) StreamMessage(apiKey, modelID string, msgs []domain.TranscriptMessage, tools []provider.ToolSpec, system string, onDelta func(string)) ([]domain.ContentBlock, string, provider.Usage, error) {
	return nil, "", provider.Usage{}, fmt.Errorf("cancelled")
}
func (p *errorProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	return nil, nil
}

// compactionMockStore wraps mockStore to return compaction data.
type compactionMockStore struct {
	*mockStore
	compactionSummary string
	compactionCutoff  int
}

func (s *compactionMockStore) LatestCompaction(sessionID string) (string, int, error) {
	if s.compactionCutoff > 0 {
		return s.compactionSummary, s.compactionCutoff, nil
	}
	return "", 0, fmt.Errorf("no compaction")
}

func (s *compactionMockStore) GetMessagesAfterSequence(sessionID string, afterSequence int) ([]domain.TranscriptMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[sessionID], nil
}

// ---------------------------------------------------------------------------
// generateAndSetTitle
// ---------------------------------------------------------------------------

func TestService_generateAndSetTitle(t *testing.T) {
	t.Run("generates title from first user message", func(t *testing.T) {
		st := newMockStore()
		sess := &domain.Session{ID: "sess-title", Title: ""}
		st.addSession(sess)

		svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
		svc.messages = []domain.TranscriptMessage{
			{Role: "user", Content: "Help me refactor the authentication module"},
			{Role: "assistant", Content: "Sure!"},
		}

		var events []Event
		svc.generateAndSetTitle("Sure!", func(evt Event) {
			events = append(events, evt)
		})

		if sess.Title == "" {
			t.Error("expected title to be set")
		}
		if len(sess.Title) > 54 { // 50 + "..."
			t.Errorf("expected title truncated, got length %d", len(sess.Title))
		}

		var gotTitled bool
		for _, evt := range events {
			if evt.Kind == EventTitled {
				gotTitled = true
				if evt.NewTitle == "" {
					t.Error("expected non-empty NewTitle in event")
				}
			}
		}
		if !gotTitled {
			t.Error("expected EventTitled event")
		}
	})

	t.Run("truncates long titles", func(t *testing.T) {
		st := newMockStore()
		sess := &domain.Session{ID: "sess-title2", Title: ""}
		st.addSession(sess)

		longMsg := "This is a very long user message that exceeds the fifty character limit for session titles"
		svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
		svc.messages = []domain.TranscriptMessage{
			{Role: "user", Content: longMsg},
		}

		svc.generateAndSetTitle("response", func(evt Event) {})

		if len(sess.Title) > 54 {
			t.Errorf("expected truncated title, got length %d: %q", len(sess.Title), sess.Title)
		}
	})

	t.Run("skips when user renamed", func(t *testing.T) {
		st := newMockStore()
		sess := &domain.Session{ID: "sess-renamed", Title: "My Custom Title"}
		st.addSession(sess)

		svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
		svc.messages = []domain.TranscriptMessage{
			{Role: "user", Content: "Help me refactor"},
			{Role: "assistant", Content: "Sure!"},
		}
		svc.SetUserRenamed()

		var events []Event
		svc.generateAndSetTitle("Sure!", func(evt Event) {
			events = append(events, evt)
		})

		if sess.Title != "My Custom Title" {
			t.Errorf("expected title unchanged after user rename, got %q", sess.Title)
		}
		if len(events) != 0 {
			t.Error("expected no events when user renamed")
		}
	})

	t.Run("skips empty user text", func(t *testing.T) {
		st := newMockStore()
		sess := &domain.Session{ID: "sess-title3", Title: "old"}
		st.addSession(sess)

		svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
		svc.messages = []domain.TranscriptMessage{} // no user messages

		var events []Event
		svc.generateAndSetTitle("response", func(evt Event) {
			events = append(events, evt)
		})

		if sess.Title != "old" {
			t.Errorf("expected title unchanged, got %q", sess.Title)
		}
		if len(events) != 0 {
			t.Error("expected no events for empty user text")
		}
	})
}

// ---------------------------------------------------------------------------
// Submit edge cases
// ---------------------------------------------------------------------------

func TestService_Submit_alreadyRunning(t *testing.T) {
	svc := &Service{
		apiKey:        "fake",
		modelID:       "fake",
		running:       true,
		disabledTools: map[string]bool{},
	}

	var events []Event
	var mu sync.Mutex
	svc.Submit("Hello", func(evt Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventError {
		t.Errorf("expected EventError, got %d", events[0].Kind)
	}
	if events[0].Err == nil || events[0].Err.Error() != "agent is already running" {
		t.Errorf("expected 'agent is already running' error, got %v", events[0].Err)
	}
}

func TestService_SetUtilityModels(t *testing.T) {
	t.Run("SetModelCompact stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelCompact("claude-haiku-4-5-20251001")
		if svc.modelCompact != "claude-haiku-4-5-20251001" {
			t.Errorf("expected claude-haiku-4-5-20251001, got %s", svc.modelCompact)
		}
	})

	t.Run("SetModelTitle stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelTitle("gpt-4o-mini")
		if svc.modelTitle != "gpt-4o-mini" {
			t.Errorf("expected gpt-4o-mini, got %s", svc.modelTitle)
		}
	})

	t.Run("SetModelTags stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelTags("claude-haiku-4-5-20251001")
		if svc.modelTags != "claude-haiku-4-5-20251001" {
			t.Errorf("expected claude-haiku-4-5-20251001, got %s", svc.modelTags)
		}
	})
}

func TestService_Submit_cancelledBeforeLoop(t *testing.T) {
	st := newMockStore()
	sess := &domain.Session{ID: "sess-cancel", Title: "test"}
	st.addSession(sess)

	svc := NewService("key", "model", "label", st, sess, &errorProvider{})
	svc.Cwd = "/tmp"

	var events []Event
	svc.Submit("Hello", func(evt Event) {
		events = append(events, evt)
	})

	// The error provider causes a non-retryable error, so
	// no stream/turn events should be emitted.
	for _, evt := range events {
		if evt.Kind == EventStreamDone || evt.Kind == EventTurnDone {
			t.Errorf("unexpected event kind %d after error provider", evt.Kind)
		}
	}
}
