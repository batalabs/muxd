package agent

import (
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// TestCompactSummaryPrompt
// ---------------------------------------------------------------------------

func TestCompactSummaryPrompt(t *testing.T) {
	requiredSections := []string{
		"## Decisions made",
		"## Files changed",
		"## Current plan",
		"## Key constraints",
		"## Errors encountered",
	}
	for _, section := range requiredSections {
		t.Run("contains "+section, func(t *testing.T) {
			if !strings.Contains(compactSummaryPrompt, section) {
				t.Errorf("compactSummaryPrompt missing required section %q", section)
			}
		})
	}
}

func TestSummarizationModel(t *testing.T) {
	t.Run("defaults to main model for anthropic", func(t *testing.T) {
		svc := &Service{
			modelID: "claude-sonnet-4-20250514",
			prov:    &fakeProvider{name: "anthropic"},
		}
		got := svc.summarizationModel()
		if got != "claude-sonnet-4-20250514" {
			t.Errorf("expected main model, got %s", got)
		}
	})

	t.Run("defaults to main model for openai", func(t *testing.T) {
		svc := &Service{
			modelID: "gpt-4o",
			prov:    &fakeProvider{name: "openai"},
		}
		got := svc.summarizationModel()
		if got != "gpt-4o" {
			t.Errorf("expected main model, got %s", got)
		}
	})

	t.Run("defaults to main model for unknown provider", func(t *testing.T) {
		svc := &Service{
			modelID: "custom-model",
			prov:    &fakeProvider{name: "ollama"},
		}
		got := svc.summarizationModel()
		if got != "custom-model" {
			t.Errorf("expected main model, got %s", got)
		}
	})

	t.Run("defaults to main model when no provider", func(t *testing.T) {
		svc := &Service{modelID: "my-model"}
		got := svc.summarizationModel()
		if got != "my-model" {
			t.Errorf("expected main model, got %s", got)
		}
	})

	t.Run("returns modelCompact when set", func(t *testing.T) {
		svc := &Service{
			modelCompact: "custom-cheap-model",
			modelID:      "claude-opus-4-6",
			prov:         &fakeProvider{name: "anthropic"},
		}
		got := svc.summarizationModel()
		if got != "custom-cheap-model" {
			t.Errorf("expected custom-cheap-model, got %s", got)
		}
	})

	t.Run("modelCompact overrides provider default", func(t *testing.T) {
		svc := &Service{
			modelCompact: "claude-sonnet-4-6",
			modelID:      "claude-opus-4-6",
			prov:         &fakeProvider{name: "anthropic"},
		}
		got := svc.summarizationModel()
		// Should use modelCompact, not the default haiku
		if got != "claude-sonnet-4-6" {
			t.Errorf("expected claude-sonnet-4-6, got %s", got)
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
		for i := 0; i < CompactKeepTail+10; i++ {
			st.messages[sess.ID] = append(st.messages[sess.ID], domain.TranscriptMessage{
				Role:    "user",
				Content: "msg",
			})
		}

		svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
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
		// No messages -maxSeq will be 0

		svc := NewService("key", "model", "label", st, sess, &fakeProvider{name: "test"})
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
		// All user messages -headEnd should be 1
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

// fakeProvider is defined in session_test.go -the compiler sees it package-wide.
// We need a provider for summarizationModel tests.
var _ provider.Provider = (*fakeProvider)(nil)

// ---------------------------------------------------------------------------
// TestSummarizeToolResult
// ---------------------------------------------------------------------------

func TestSummarizeToolResult(t *testing.T) {
	cases := []struct {
		name       string
		toolName   string
		result     string
		wantPfx    string // expected substring
		wantAbsent string // must NOT appear in output
	}{
		{
			name:     "file_read with newlines",
			toolName: "file_read",
			result:   "line1\nline2\nline3\n",
			wantPfx:  "[read: 3 lines]",
		},
		{
			name:     "file_read single line",
			toolName: "file_read",
			result:   "hello",
			wantPfx:  "[read: 1 lines]",
		},
		{
			name:     "file_read empty",
			toolName: "file_read",
			result:   "",
			wantPfx:  "[read: 0 lines]",
		},
		{
			name:     "file_write",
			toolName: "file_write",
			result:   "wrote 42 bytes\nsome extra info",
			wantPfx:  "[wrote: wrote 42 bytes]",
		},
		{
			name:     "file_edit",
			toolName: "file_edit",
			result:   "edited /path/to/file.go\nsome extra",
			wantPfx:  "[edited: edited /path/to/file.go]",
		},
		{
			name:     "bash short",
			toolName: "bash",
			result:   "ok",
			wantPfx:  "[bash: ok]",
		},
		{
			name:     "bash truncates at 80",
			toolName: "bash",
			// 85 chars total — truncated to 80, so "EXTRA" is cut off
			result:     "01234567890123456789012345678901234567890123456789012345678901234567890123456789EXTRA",
			wantPfx:    "[bash: 01234567890123456789",
			wantAbsent: "EXTRA",
		},
		{
			name:     "grep",
			toolName: "grep",
			result:   "match1\nmatch2\nmatch3\n",
			wantPfx:  "[grep: 3 matches]",
		},
		{
			name:     "grep empty",
			toolName: "grep",
			result:   "",
			wantPfx:  "[grep: 0 matches]",
		},
		{
			name:     "glob",
			toolName: "glob",
			result:   "file1.go\nfile2.go\n",
			wantPfx:  "[glob: 2 files]",
		},
		{
			name:     "list_files",
			toolName: "list_files",
			result:   "a\nb\nc\n",
			wantPfx:  "[list_files: 3 files]",
		},
		{
			name:     "web_fetch",
			toolName: "web_fetch",
			result:   "some content here",
			wantPfx:  "[fetched: 17 chars]",
		},
		{
			name:     "web_search",
			toolName: "web_search",
			result:   "result1\nresult2\n",
			wantPfx:  "[search: 2 results]",
		},
		{
			name:     "other tool short",
			toolName: "custom_tool",
			result:   "hello world",
			wantPfx:  "[custom_tool: hello world]",
		},
		{
			name:     "other tool truncates at 100",
			toolName: "custom_tool",
			result:   "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789EXTRA_CHARS_HERE_012345678",
			wantPfx:  "[custom_tool:",
		},
		{
			name:     "other tool replaces newlines",
			toolName: "custom_tool",
			result:   "line1\nline2\nline3",
			wantPfx:  "[custom_tool: line1 line2 line3]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeToolResult(tc.toolName, tc.result)

			// Must be single-line
			if containsNewline(got) {
				t.Errorf("summarizeToolResult returned multi-line string: %q", got)
			}

			// Must be under 200 chars
			if len(got) >= 200 {
				t.Errorf("summarizeToolResult too long (%d chars): %q", len(got), got)
			}

			// Must contain expected prefix/content
			if !containsStr(got, tc.wantPfx) {
				t.Errorf("expected %q to contain %q", got, tc.wantPfx)
			}

			// Must not contain absent string
			if tc.wantAbsent != "" && containsStr(got, tc.wantAbsent) {
				t.Errorf("expected %q NOT to contain %q", got, tc.wantAbsent)
			}
		})
	}
}

func containsNewline(s string) bool {
	for _, c := range s {
		if c == '\n' || c == '\r' {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub)))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// TestCompressTier1
// ---------------------------------------------------------------------------

func TestCompressTier1(t *testing.T) {
	t.Run("compresses long tool results before tailStart", func(t *testing.T) {
		longResult := make([]byte, 300)
		for i := range longResult {
			longResult[i] = 'x'
		}

		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "do something"},
			{
				Role: "assistant",
				Blocks: []domain.ContentBlock{
					{Type: "tool_use", ToolUseID: "u1", ToolName: "bash"},
				},
			},
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "bash", ToolResult: string(longResult)},
				},
			},
			{Role: "assistant", Content: "done"},
		}

		tailStart := 3 // last "done" message is in tail
		got := compressTier1(msgs, tailStart)

		if len(got) != len(msgs) {
			t.Fatalf("expected same number of messages, got %d", len(got))
		}

		// The tool_result block in msg[2] (before tailStart) must be summarized
		block := got[2].Blocks[0]
		if len(block.ToolResult) >= 200 {
			t.Errorf("expected tool result compressed, got len=%d: %q", len(block.ToolResult), block.ToolResult)
		}
		if block.ToolResult == string(longResult) {
			t.Error("expected tool result to be replaced with summary, not original")
		}
	})

	t.Run("does not compress short tool results", func(t *testing.T) {
		shortResult := "short output"
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "do something"},
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "bash", ToolResult: shortResult},
				},
			},
			{Role: "assistant", Content: "done"},
		}

		tailStart := 2
		got := compressTier1(msgs, tailStart)

		block := got[1].Blocks[0]
		if block.ToolResult != shortResult {
			t.Errorf("expected short tool result unchanged, got %q", block.ToolResult)
		}
	})

	t.Run("does not mutate original slice", func(t *testing.T) {
		longResult := make([]byte, 300)
		for i := range longResult {
			longResult[i] = 'y'
		}

		msgs := []domain.TranscriptMessage{
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "file_read", ToolResult: string(longResult)},
				},
			},
			{Role: "assistant", Content: "ok"},
		}

		original := make([]domain.TranscriptMessage, len(msgs))
		copy(original, msgs)
		// Deep copy blocks
		origBlocks := make([]domain.ContentBlock, len(msgs[0].Blocks))
		copy(origBlocks, msgs[0].Blocks)
		originalBlockResult := origBlocks[0].ToolResult

		tailStart := 1
		compressTier1(msgs, tailStart)

		// Original must be unchanged
		if msgs[0].Blocks[0].ToolResult != originalBlockResult {
			t.Error("compressTier1 mutated the original message slice")
		}
	})
}

// ---------------------------------------------------------------------------
// TestCompressTier2
// ---------------------------------------------------------------------------

func TestCompressTier2(t *testing.T) {
	t.Run("compresses head into bullet summary, tail preserved", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "Hello, can you help me?"},
			{Role: "assistant", Content: "Sure, I can help."},
			{Role: "user", Content: "Please write a function"},
			{Role: "assistant", Content: "Here is the function"},
			// tail starts here (tailStart=4)
			{Role: "user", Content: "Now add tests"},
			{Role: "assistant", Content: "Adding tests now"},
		}
		tailStart := 4
		got := compressTier2(msgs, tailStart)

		// Should have: 1 compressed user msg + 1 ack + 2 tail = 4 messages
		if len(got) != 4 {
			t.Fatalf("expected 4 messages, got %d", len(got))
		}

		// First message must be the compressed summary (user role)
		if got[0].Role != "user" {
			t.Errorf("expected role=user for compressed summary, got %q", got[0].Role)
		}
		if !containsSubstr(got[0].Content, "[Compressed conversation history]") {
			t.Errorf("expected compressed header in first message, got %q", got[0].Content)
		}
		if !containsSubstr(got[0].Content, "- User:") {
			t.Errorf("expected '- User:' bullet in compressed summary, got %q", got[0].Content)
		}
		if !containsSubstr(got[0].Content, "  Agent:") {
			t.Errorf("expected '  Agent:' bullet in compressed summary, got %q", got[0].Content)
		}

		// Second message must be the assistant acknowledgment
		if got[1].Role != "assistant" {
			t.Errorf("expected role=assistant for ack, got %q", got[1].Role)
		}
		if got[1].Content != "Understood. I have the conversation context above." {
			t.Errorf("unexpected ack content: %q", got[1].Content)
		}

		// Tail messages must be untouched
		if got[2].Content != "Now add tests" {
			t.Errorf("expected tail[0] = 'Now add tests', got %q", got[2].Content)
		}
		if got[3].Content != "Adding tests now" {
			t.Errorf("expected tail[1] = 'Adding tests now', got %q", got[3].Content)
		}
	})

	t.Run("fewer messages than original", func(t *testing.T) {
		var msgs []domain.TranscriptMessage
		for i := 0; i < 10; i++ {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			msgs = append(msgs, domain.TranscriptMessage{Role: role, Content: "message content"})
		}
		tailStart := 6
		got := compressTier2(msgs, tailStart)

		// Original had 10 messages; result should have compressed_user + ack + 4 tail = 6
		if len(got) >= len(msgs) {
			t.Errorf("expected fewer messages after compression, got %d (original %d)", len(got), len(msgs))
		}
	})

	t.Run("truncates long content to 120 chars", func(t *testing.T) {
		long := make([]byte, 200)
		for i := range long {
			long[i] = 'a'
		}
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: string(long)},
			{Role: "assistant", Content: string(long)},
			{Role: "user", Content: "tail message"},
		}
		tailStart := 2
		got := compressTier2(msgs, tailStart)

		// The summary should not contain the full 200-char string
		summary := got[0].Content
		// Each line for user/assistant should be max ~120 chars + prefix overhead
		for _, line := range strings.Split(summary, "\n") {
			if containsSubstr(line, "- User:") || containsSubstr(line, "  Agent:") {
				// The total line length should not exceed 120 + prefix
				if len(line) > 140 {
					t.Errorf("summary line too long (%d chars): %q", len(line), line)
				}
			}
		}
	})

	t.Run("newlines replaced in content", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "line one\nline two\nline three"},
			{Role: "assistant", Content: "response\nwith\nnewlines"},
			{Role: "user", Content: "tail"},
		}
		tailStart := 2
		got := compressTier2(msgs, tailStart)

		summary := got[0].Content
		for _, line := range strings.Split(summary, "\n") {
			if containsSubstr(line, "- User:") || containsSubstr(line, "  Agent:") {
				// The bullet line itself should not contain embedded newlines
				// (we just check the extracted content portion has no \n)
				_ = line // single lines by definition
			}
		}
		// The summary content should replace \n with space
		if containsSubstr(got[0].Content, "line one\nline two") {
			t.Error("expected newlines replaced in summary content")
		}
	})
}

func TestCompressTier2_emptyHead(t *testing.T) {
	t.Run("tailStart=0 returns messages unchanged", func(t *testing.T) {
		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "second"},
			{Role: "user", Content: "third"},
		}
		got := compressTier2(msgs, 0)
		if len(got) != len(msgs) {
			t.Fatalf("expected %d messages, got %d", len(msgs), len(got))
		}
		for i, m := range got {
			if m.Role != msgs[i].Role || m.Content != msgs[i].Content {
				t.Errorf("message[%d] changed: got %+v, want %+v", i, m, msgs[i])
			}
		}
	})
}

func TestCompressTier1_preservesTail(t *testing.T) {
	t.Run("tool result in tail stays full", func(t *testing.T) {
		longResult := make([]byte, 300)
		for i := range longResult {
			longResult[i] = 'z'
		}

		msgs := []domain.TranscriptMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "second"},
			// tailStart = 2: everything from here is tail
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "bash", ToolResult: string(longResult)},
				},
			},
		}

		tailStart := 2
		got := compressTier1(msgs, tailStart)

		block := got[2].Blocks[0]
		if block.ToolResult != string(longResult) {
			t.Errorf("expected tail tool result to be preserved, got len=%d", len(block.ToolResult))
		}
	})

	t.Run("tailStart=0 preserves all messages", func(t *testing.T) {
		longResult := make([]byte, 300)
		for i := range longResult {
			longResult[i] = 'w'
		}

		msgs := []domain.TranscriptMessage{
			{
				Role: "user",
				Blocks: []domain.ContentBlock{
					{Type: "tool_result", ToolUseID: "u1", ToolName: "bash", ToolResult: string(longResult)},
				},
			},
		}

		got := compressTier1(msgs, 0)

		block := got[0].Blocks[0]
		if block.ToolResult != string(longResult) {
			t.Errorf("expected all preserved when tailStart=0, got %q", block.ToolResult)
		}
	})
}
