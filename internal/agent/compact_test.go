package agent

import (
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

func TestSummarizationModel(t *testing.T) {
	t.Run("returns haiku for anthropic", func(t *testing.T) {
		svc := &Service{
			modelID: "claude-sonnet-4-20250514",
			prov:    &fakeProvider{name: "anthropic"},
		}
		got := svc.summarizationModel()
		if got != "claude-haiku-4-5-20251001" {
			t.Errorf("expected haiku model, got %s", got)
		}
	})

	t.Run("returns gpt-4o-mini for openai", func(t *testing.T) {
		svc := &Service{
			modelID: "gpt-4o",
			prov:    &fakeProvider{name: "openai"},
		}
		got := svc.summarizationModel()
		if got != "gpt-4o-mini" {
			t.Errorf("expected gpt-4o-mini, got %s", got)
		}
	})

	t.Run("falls back to current model for unknown provider", func(t *testing.T) {
		svc := &Service{
			modelID: "custom-model",
			prov:    &fakeProvider{name: "ollama"},
		}
		got := svc.summarizationModel()
		if got != "custom-model" {
			t.Errorf("expected current model fallback, got %s", got)
		}
	})

	t.Run("falls back to current model when no provider", func(t *testing.T) {
		svc := &Service{modelID: "my-model"}
		got := svc.summarizationModel()
		if got != "my-model" {
			t.Errorf("expected current model fallback, got %s", got)
		}
	})
}

func TestPersistCompaction(t *testing.T) {
	t.Run("skips when no store", func(t *testing.T) {
		svc := &Service{session: &domain.Session{ID: "s1"}}
		// Should not panic
		svc.persistCompaction("summary")
	})

	t.Run("skips when no session", func(t *testing.T) {
		svc := &Service{store: newMockStore()}
		// Should not panic
		svc.persistCompaction("summary")
	})

	t.Run("saves compaction to store", func(t *testing.T) {
		st := &persistCompactionMock{
			mockStore: newMockStore(),
		}
		sess := &domain.Session{ID: "sess-persist"}
		st.addSession(sess)
		// Add enough messages to have a cutoff > 0
		for i := 0; i < CompactKeepTail + 10; i++ {
			st.messages[sess.ID] = append(st.messages[sess.ID], domain.TranscriptMessage{
				Role:    "user",
				Content: "msg",
			})
		}

		svc := NewService("key", "model", "label", st, sess, nil)
		svc.persistCompaction("test summary")

		if !st.savedCalled {
			t.Error("expected SaveCompaction to be called")
		}
		if st.savedSummary != "test summary" {
			t.Errorf("expected saved summary 'test summary', got %q", st.savedSummary)
		}
	})

	t.Run("skips when maxSeq is zero", func(t *testing.T) {
		st := &persistCompactionMock{
			mockStore: newMockStore(),
		}
		sess := &domain.Session{ID: "sess-empty"}
		st.addSession(sess)
		// No messages — maxSeq will be 0

		svc := NewService("key", "model", "label", st, sess, nil)
		svc.persistCompaction("summary")

		if st.savedCalled {
			t.Error("SaveCompaction should not be called when maxSeq=0")
		}
	})
}

func TestSummarizeToolInput(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		got := summarizeToolInput(nil)
		if got != "{}" {
			t.Errorf("expected {}, got %q", got)
		}
	})

	t.Run("simple input", func(t *testing.T) {
		got := summarizeToolInput(map[string]any{"path": "/tmp/file.go"})
		if got == "" {
			t.Error("expected non-empty result")
		}
	})
}

func TestCompactMessages_edgeCases(t *testing.T) {
	t.Run("tailStart equals headEnd returns no compaction", func(t *testing.T) {
		// Exactly CompactKeepTail+2 messages (boundary)
		var msgs []domain.TranscriptMessage
		msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: "q"})
		msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: "a"})
		for i := 0; i < CompactKeepTail; i++ {
			if i%2 == 0 {
				msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: "q"})
			} else {
				msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: "a"})
			}
		}
		result := CompactMessages(msgs)
		if result.DidCompact {
			t.Error("expected no compaction at exact boundary")
		}
	})

	t.Run("no assistant in head", func(t *testing.T) {
		// All user messages — headEnd should be 1
		var msgs []domain.TranscriptMessage
		for i := 0; i < CompactKeepTail+5; i++ {
			if i%2 == 0 {
				msgs = append(msgs, domain.TranscriptMessage{Role: "user", Content: "q"})
			} else {
				msgs = append(msgs, domain.TranscriptMessage{Role: "assistant", Content: "a"})
			}
		}
		// Remove the first assistant to test the headEnd=1 branch
		msgs[1] = domain.TranscriptMessage{Role: "user", Content: "also user"}

		result := CompactMessages(msgs)
		// Should still produce valid output
		if result.DidCompact && len(result.Messages) == 0 {
			t.Error("compaction produced empty messages")
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type persistCompactionMock struct {
	*mockStore
	savedCalled  bool
	savedSummary string
	savedCutoff  int
}

func (s *persistCompactionMock) SaveCompaction(sessionID, summaryText string, cutoffSequence int) error {
	s.savedCalled = true
	s.savedSummary = summaryText
	s.savedCutoff = cutoffSequence
	return nil
}

func (s *persistCompactionMock) MessageMaxSequence(sessionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages[sessionID]), nil
}

// fakeProvider is defined in session_test.go — the compiler sees it package-wide.
// We need a provider for summarizationModel tests.
var _ provider.Provider = (*fakeProvider)(nil)
