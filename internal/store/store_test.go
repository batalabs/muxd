package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/batalabs/muxd/internal/domain"

	_ "modernc.org/sqlite"
)

// testStore returns a Store backed by an in-memory SQLite database.
func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	s, err := NewFromDB(db)
	if err != nil {
		db.Close()
		t.Fatalf("new store from db: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_CreateSession(t *testing.T) {
	s := testStore(t)

	t.Run("creates session with correct fields", func(t *testing.T) {
		sess, err := s.CreateSession("/tmp/project", "claude-sonnet-4-20250514")
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if sess.ID == "" {
			t.Error("expected non-empty session ID")
		}
		if sess.ProjectPath != "/tmp/project" {
			t.Errorf("ProjectPath = %q, want %q", sess.ProjectPath, "/tmp/project")
		}
		if sess.Title != "New Session" {
			t.Errorf("Title = %q, want %q", sess.Title, "New Session")
		}
		if sess.Model != "claude-sonnet-4-20250514" {
			t.Errorf("Model = %q, want %q", sess.Model, "claude-sonnet-4-20250514")
		}
	})

	t.Run("creates unique IDs", func(t *testing.T) {
		s1, err := s.CreateSession("/tmp", "m1")
		if err != nil {
			t.Fatalf("CreateSession 1: %v", err)
		}
		s2, err := s.CreateSession("/tmp", "m2")
		if err != nil {
			t.Fatalf("CreateSession 2: %v", err)
		}
		if s1.ID == s2.ID {
			t.Error("expected different session IDs")
		}
	})
}

func TestStore_GetSession(t *testing.T) {
	s := testStore(t)

	t.Run("returns session by ID", func(t *testing.T) {
		created, err := s.CreateSession("/tmp/project", "model-1")
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		got, err := s.GetSession(created.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("ID = %q, want %q", got.ID, created.ID)
		}
		if got.ProjectPath != "/tmp/project" {
			t.Errorf("ProjectPath = %q, want %q", got.ProjectPath, "/tmp/project")
		}
		if got.Model != "model-1" {
			t.Errorf("Model = %q, want %q", got.Model, "model-1")
		}
	})

	t.Run("returns error for nonexistent ID", func(t *testing.T) {
		_, err := s.GetSession("nonexistent-id")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestStore_LatestSession(t *testing.T) {
	s := testStore(t)

	t.Run("returns most recently updated session", func(t *testing.T) {
		s1, err := s.CreateSession("/tmp/project", "m1")
		if err != nil {
			t.Fatalf("CreateSession 1: %v", err)
		}
		s2, err := s.CreateSession("/tmp/project", "m2")
		if err != nil {
			t.Fatalf("CreateSession 2: %v", err)
		}

		// Touch s1 so it becomes the latest.
		if err := s.TouchSession(s1.ID); err != nil {
			t.Fatalf("TouchSession: %v", err)
		}
		_ = s2 // silence unused warning

		latest, err := s.LatestSession("/tmp/project")
		if err != nil {
			t.Fatalf("LatestSession: %v", err)
		}
		if latest.ID != s1.ID {
			t.Errorf("LatestSession ID = %q, want %q", latest.ID, s1.ID)
		}
	})

	t.Run("returns error when no sessions exist", func(t *testing.T) {
		_, err := s.LatestSession("/nonexistent/path")
		if err == nil {
			t.Error("expected error for nonexistent project path")
		}
	})
}

func TestStore_ListSessions(t *testing.T) {
	s := testStore(t)

	t.Run("returns sessions for project path", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if _, err := s.CreateSession("/tmp/project", "model"); err != nil {
				t.Fatalf("CreateSession %d: %v", i, err)
			}
		}
		// Create one in a different project.
		if _, err := s.CreateSession("/tmp/other", "model"); err != nil {
			t.Fatalf("CreateSession other: %v", err)
		}

		sessions, err := s.ListSessions("/tmp/project", 10)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) != 3 {
			t.Errorf("got %d sessions, want 3", len(sessions))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		sessions, err := s.ListSessions("/tmp/project", 2)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) != 2 {
			t.Errorf("got %d sessions, want 2", len(sessions))
		}
	})

	t.Run("defaults limit to 10 when zero", func(t *testing.T) {
		sessions, err := s.ListSessions("/tmp/project", 0)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) != 3 {
			t.Errorf("got %d sessions, want 3", len(sessions))
		}
	})

	t.Run("returns empty slice for nonexistent project", func(t *testing.T) {
		sessions, err := s.ListSessions("/nonexistent", 10)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) != 0 {
			t.Errorf("got %d sessions, want 0", len(sessions))
		}
	})
}

func TestStore_UpdateSessionTitle(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("updates title", func(t *testing.T) {
		if err := s.UpdateSessionTitle(sess.ID, "My New Title"); err != nil {
			t.Fatalf("UpdateSessionTitle: %v", err)
		}
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.Title != "My New Title" {
			t.Errorf("Title = %q, want %q", got.Title, "My New Title")
		}
	})
}

func TestStore_UpdateSessionTokens(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("updates token counts", func(t *testing.T) {
		if err := s.UpdateSessionTokens(sess.ID, 100, 200); err != nil {
			t.Fatalf("UpdateSessionTokens: %v", err)
		}
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.TotalTokens != 300 {
			t.Errorf("TotalTokens = %d, want 300", got.TotalTokens)
		}
		if got.InputTokens != 100 {
			t.Errorf("InputTokens = %d, want 100", got.InputTokens)
		}
		if got.OutputTokens != 200 {
			t.Errorf("OutputTokens = %d, want 200", got.OutputTokens)
		}
	})
}

func TestStore_UpdateSessionModel(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model-old")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("updates model", func(t *testing.T) {
		if err := s.UpdateSessionModel(sess.ID, "model-new"); err != nil {
			t.Fatalf("UpdateSessionModel: %v", err)
		}
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.Model != "model-new" {
			t.Errorf("Model = %q, want %q", got.Model, "model-new")
		}
	})
}

func TestStore_TouchSession(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("updates timestamp without error", func(t *testing.T) {
		if err := s.TouchSession(sess.ID); err != nil {
			t.Fatalf("TouchSession: %v", err)
		}
	})
}

func TestStore_AppendMessage(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("appends and retrieves plain-text message", func(t *testing.T) {
		if err := s.AppendMessage(sess.ID, "user", "hello", 10); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		if err := s.AppendMessage(sess.ID, "assistant", "hi there", 20); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}

		msgs, err := s.GetMessages(sess.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		if msgs[0].Role != "user" || msgs[0].Content != "hello" {
			t.Errorf("msg[0] = {%q, %q}, want {user, hello}", msgs[0].Role, msgs[0].Content)
		}
		if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
			t.Errorf("msg[1] = {%q, %q}, want {assistant, hi there}", msgs[1].Role, msgs[1].Content)
		}
	})

	t.Run("updates message count on session", func(t *testing.T) {
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.MessageCount != 2 {
			t.Errorf("MessageCount = %d, want 2", got.MessageCount)
		}
	})
}

func TestStore_AppendMessageBlocks(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("appends and retrieves block-based message", func(t *testing.T) {
		blocks := []domain.ContentBlock{
			{Type: "text", Text: "Here is the result:"},
			{Type: "tool_use", ToolUseID: "tu_1", ToolName: "read_file", ToolInput: map[string]any{"path": "main.go"}},
		}
		if err := s.AppendMessageBlocks(sess.ID, "assistant", blocks, 50); err != nil {
			t.Fatalf("AppendMessageBlocks: %v", err)
		}

		msgs, err := s.GetMessages(sess.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
		msg := msgs[0]
		if msg.Role != "assistant" {
			t.Errorf("Role = %q, want %q", msg.Role, "assistant")
		}
		if len(msg.Blocks) != 2 {
			t.Fatalf("got %d blocks, want 2", len(msg.Blocks))
		}
		if msg.Blocks[0].Type != "text" || msg.Blocks[0].Text != "Here is the result:" {
			t.Errorf("block[0] = %+v, want text block", msg.Blocks[0])
		}
		if msg.Blocks[1].Type != "tool_use" || msg.Blocks[1].ToolName != "read_file" {
			t.Errorf("block[1] = %+v, want tool_use block", msg.Blocks[1])
		}
		// Content should be the concatenation of text blocks.
		if msg.Content != "Here is the result:" {
			t.Errorf("Content = %q, want %q", msg.Content, "Here is the result:")
		}
	})
}

func TestStore_GetMessages_empty(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("returns empty slice for session with no messages", func(t *testing.T) {
		msgs, err := s.GetMessages(sess.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 0 {
			t.Errorf("got %d messages, want 0", len(msgs))
		}
	})
}

func TestStore_SessionTitle(t *testing.T) {
	s := testStore(t)

	t.Run("returns title for existing session", func(t *testing.T) {
		sess, err := s.CreateSession("/tmp", "model")
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if err := s.UpdateSessionTitle(sess.ID, "Test Title"); err != nil {
			t.Fatalf("UpdateSessionTitle: %v", err)
		}
		got := s.SessionTitle(sess.ID)
		if got != "Test Title" {
			t.Errorf("SessionTitle = %q, want %q", got, "Test Title")
		}
	})

	t.Run("returns Unknown for nonexistent session", func(t *testing.T) {
		got := s.SessionTitle("nonexistent")
		if got != "Unknown" {
			t.Errorf("SessionTitle = %q, want %q", got, "Unknown")
		}
	})
}

func TestStore_FindSessionByPrefix(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("finds session by ID prefix", func(t *testing.T) {
		prefix := sess.ID[:8]
		got, err := s.FindSessionByPrefix(prefix)
		if err != nil {
			t.Fatalf("FindSessionByPrefix: %v", err)
		}
		if got.ID != sess.ID {
			t.Errorf("ID = %q, want %q", got.ID, sess.ID)
		}
	})

	t.Run("returns error for unmatched prefix", func(t *testing.T) {
		_, err := s.FindSessionByPrefix("zzzzzzzzz")
		if err == nil {
			t.Error("expected error for unmatched prefix")
		}
	})
}

func TestStore_migrate_idempotent(t *testing.T) {
	s := testStore(t)

	t.Run("running migrate twice does not error", func(t *testing.T) {
		if err := s.migrate(); err != nil {
			t.Fatalf("second migrate: %v", err)
		}
	})
}

func TestStore_MessageSequencing(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("messages are returned in insertion order", func(t *testing.T) {
		for i, text := range []string{"first", "second", "third"} {
			if err := s.AppendMessage(sess.ID, "user", text, i*10); err != nil {
				t.Fatalf("AppendMessage %q: %v", text, err)
			}
		}

		msgs, err := s.GetMessages(sess.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3", len(msgs))
		}
		for i, want := range []string{"first", "second", "third"} {
			if msgs[i].Content != want {
				t.Errorf("msgs[%d].Content = %q, want %q", i, msgs[i].Content, want)
			}
		}
	})
}

func TestStore_NewFieldDefaults(t *testing.T) {
	s := testStore(t)

	t.Run("new session has empty parent and tags", func(t *testing.T) {
		sess, err := s.CreateSession("/tmp/project", "model")
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.ParentSessionID != "" {
			t.Errorf("ParentSessionID = %q, want empty", got.ParentSessionID)
		}
		if got.BranchPoint != 0 {
			t.Errorf("BranchPoint = %d, want 0", got.BranchPoint)
		}
		if got.Tags != "" {
			t.Errorf("Tags = %q, want empty", got.Tags)
		}
	})
}

func TestStore_UpdateSessionTags(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("sets and retrieves tags", func(t *testing.T) {
		if err := s.UpdateSessionTags(sess.ID, "bugfix,refactor"); err != nil {
			t.Fatalf("UpdateSessionTags: %v", err)
		}
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.Tags != "bugfix,refactor" {
			t.Errorf("Tags = %q, want %q", got.Tags, "bugfix,refactor")
		}
	})

	t.Run("overwrites existing tags", func(t *testing.T) {
		if err := s.UpdateSessionTags(sess.ID, "feature"); err != nil {
			t.Fatalf("UpdateSessionTags: %v", err)
		}
		got, err := s.GetSession(sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.Tags != "feature" {
			t.Errorf("Tags = %q, want %q", got.Tags, "feature")
		}
	})

	t.Run("tags appear in ListSessions", func(t *testing.T) {
		sessions, err := s.ListSessions("/tmp", 10)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) == 0 {
			t.Fatal("expected at least one session")
		}
		if sessions[0].Tags != "feature" {
			t.Errorf("ListSessions Tags = %q, want %q", sessions[0].Tags, "feature")
		}
	})
}

func TestStore_MixedMessages(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("plain and block messages interleave correctly", func(t *testing.T) {
		if err := s.AppendMessage(sess.ID, "user", "hello", 5); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		blocks := []domain.ContentBlock{
			{Type: "text", Text: "response text"},
		}
		if err := s.AppendMessageBlocks(sess.ID, "assistant", blocks, 10); err != nil {
			t.Fatalf("AppendMessageBlocks: %v", err)
		}
		if err := s.AppendMessage(sess.ID, "user", "follow up", 5); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}

		msgs, err := s.GetMessages(sess.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3", len(msgs))
		}
		if msgs[0].Content != "hello" {
			t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "hello")
		}
		if msgs[1].Content != "response text" {
			t.Errorf("msgs[1].Content = %q, want %q", msgs[1].Content, "response text")
		}
		if !msgs[1].HasBlocks() {
			t.Error("msgs[1] should have blocks")
		}
		if msgs[2].Content != "follow up" {
			t.Errorf("msgs[2].Content = %q, want %q", msgs[2].Content, "follow up")
		}
	})
}

func TestStore_SaveCompaction(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("saves and retrieves compaction", func(t *testing.T) {
		if err := s.SaveCompaction(sess.ID, "summary of old messages", 10); err != nil {
			t.Fatalf("SaveCompaction: %v", err)
		}
		summary, cutoff, err := s.LatestCompaction(sess.ID)
		if err != nil {
			t.Fatalf("LatestCompaction: %v", err)
		}
		if summary != "summary of old messages" {
			t.Errorf("summary = %q, want %q", summary, "summary of old messages")
		}
		if cutoff != 10 {
			t.Errorf("cutoff = %d, want 10", cutoff)
		}
	})

	t.Run("latest compaction wins", func(t *testing.T) {
		if err := s.SaveCompaction(sess.ID, "newer summary", 20); err != nil {
			t.Fatalf("SaveCompaction: %v", err)
		}
		summary, cutoff, err := s.LatestCompaction(sess.ID)
		if err != nil {
			t.Fatalf("LatestCompaction: %v", err)
		}
		if summary != "newer summary" {
			t.Errorf("summary = %q, want %q", summary, "newer summary")
		}
		if cutoff != 20 {
			t.Errorf("cutoff = %d, want 20", cutoff)
		}
	})
}

func TestStore_LatestCompaction_noRecord(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("returns error when no compaction exists", func(t *testing.T) {
		_, _, err := s.LatestCompaction(sess.ID)
		if err == nil {
			t.Error("expected error for missing compaction")
		}
	})
}

func TestStore_GetMessagesAfterSequence(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Add 5 messages (sequences 1-5)
	for i := 1; i <= 5; i++ {
		if err := s.AppendMessage(sess.ID, "user", fmt.Sprintf("msg%d", i), 0); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	t.Run("returns messages after sequence", func(t *testing.T) {
		msgs, err := s.GetMessagesAfterSequence(sess.ID, 3)
		if err != nil {
			t.Fatalf("GetMessagesAfterSequence: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		if msgs[0].Content != "msg4" {
			t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "msg4")
		}
		if msgs[1].Content != "msg5" {
			t.Errorf("msgs[1].Content = %q, want %q", msgs[1].Content, "msg5")
		}
	})

	t.Run("returns all messages when afterSequence is 0", func(t *testing.T) {
		msgs, err := s.GetMessagesAfterSequence(sess.ID, 0)
		if err != nil {
			t.Fatalf("GetMessagesAfterSequence: %v", err)
		}
		if len(msgs) != 5 {
			t.Errorf("got %d messages, want 5", len(msgs))
		}
	})
}

func TestStore_MessageMaxSequence(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Run("returns 0 for session with no messages", func(t *testing.T) {
		seq, err := s.MessageMaxSequence(sess.ID)
		if err != nil {
			t.Fatalf("MessageMaxSequence: %v", err)
		}
		if seq != 0 {
			t.Errorf("seq = %d, want 0", seq)
		}
	})

	t.Run("returns correct max after appending messages", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if err := s.AppendMessage(sess.ID, "user", "msg", 0); err != nil {
				t.Fatalf("AppendMessage: %v", err)
			}
		}
		seq, err := s.MessageMaxSequence(sess.ID)
		if err != nil {
			t.Fatalf("MessageMaxSequence: %v", err)
		}
		if seq != 3 {
			t.Errorf("seq = %d, want 3", seq)
		}
	})
}

func TestStore_DeleteSession(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Add a message so we can verify cascade.
	if err := s.AppendMessage(sess.ID, "user", "hello", 0); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	t.Run("deletes session and cascades messages", func(t *testing.T) {
		if err := s.DeleteSession(sess.ID); err != nil {
			t.Fatalf("DeleteSession: %v", err)
		}
		_, err := s.GetSession(sess.ID)
		if err == nil {
			t.Error("expected error for deleted session")
		}
		msgs, err := s.GetMessages(sess.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 0 {
			t.Errorf("got %d messages, want 0 after cascade delete", len(msgs))
		}
	})
}

func TestStore_BranchSession(t *testing.T) {
	s := testStore(t)

	sess, err := s.CreateSession("/tmp/project", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Add 5 messages
	for i := 1; i <= 5; i++ {
		if err := s.AppendMessage(sess.ID, "user", fmt.Sprintf("msg%d", i), 0); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	t.Run("branches all messages when atSequence is 0", func(t *testing.T) {
		branched, err := s.BranchSession(sess.ID, 0)
		if err != nil {
			t.Fatalf("BranchSession: %v", err)
		}
		if branched.ID == sess.ID {
			t.Error("expected different session ID")
		}
		if branched.ParentSessionID != sess.ID {
			t.Errorf("ParentSessionID = %q, want %q", branched.ParentSessionID, sess.ID)
		}
		if branched.BranchPoint != 5 {
			t.Errorf("BranchPoint = %d, want 5", branched.BranchPoint)
		}
		if branched.MessageCount != 5 {
			t.Errorf("MessageCount = %d, want 5", branched.MessageCount)
		}
		msgs, err := s.GetMessages(branched.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 5 {
			t.Errorf("got %d messages, want 5", len(msgs))
		}
	})

	t.Run("branches at specific sequence", func(t *testing.T) {
		branched, err := s.BranchSession(sess.ID, 3)
		if err != nil {
			t.Fatalf("BranchSession: %v", err)
		}
		if branched.BranchPoint != 3 {
			t.Errorf("BranchPoint = %d, want 3", branched.BranchPoint)
		}
		if branched.MessageCount != 3 {
			t.Errorf("MessageCount = %d, want 3", branched.MessageCount)
		}
		msgs, err := s.GetMessages(branched.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 3 {
			t.Errorf("got %d messages, want 3", len(msgs))
		}
		if msgs[2].Content != "msg3" {
			t.Errorf("last message = %q, want %q", msgs[2].Content, "msg3")
		}
	})

	t.Run("returns error for invalid session", func(t *testing.T) {
		_, err := s.BranchSession("nonexistent", 0)
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})

	t.Run("inherits parent fields", func(t *testing.T) {
		if err := s.UpdateSessionTags(sess.ID, "feature,refactor"); err != nil {
			t.Fatalf("UpdateSessionTags: %v", err)
		}
		branched, err := s.BranchSession(sess.ID, 0)
		if err != nil {
			t.Fatalf("BranchSession: %v", err)
		}
		if branched.Tags != "feature,refactor" {
			t.Errorf("Tags = %q, want %q", branched.Tags, "feature,refactor")
		}
		if branched.ProjectPath != "/tmp/project" {
			t.Errorf("ProjectPath = %q, want %q", branched.ProjectPath, "/tmp/project")
		}
	})
}

func TestStore_ScheduledTweets(t *testing.T) {
	s := testStore(t)

	now := time.Now().UTC()
	id, err := s.CreateScheduledTweet("hello world", now.Add(2*time.Minute), "once")
	if err != nil {
		t.Fatalf("CreateScheduledTweet: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	items, err := s.ListScheduledTweets(10)
	if err != nil {
		t.Fatalf("ListScheduledTweets: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected scheduled tweets in list")
	}

	due, err := s.DueScheduledTweets(now.Add(5*time.Minute), 10)
	if err != nil {
		t.Fatalf("DueScheduledTweets: %v", err)
	}
	if len(due) == 0 {
		t.Fatal("expected due scheduled tweets")
	}

	if err := s.MarkScheduledTweetPosted(id, "tweet123", now); err != nil {
		t.Fatalf("MarkScheduledTweetPosted: %v", err)
	}
	if err := s.RescheduleRecurringTweet(id, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("RescheduleRecurringTweet: %v", err)
	}
	if err := s.MarkScheduledTweetFailed(id, "test failure", now); err != nil {
		t.Fatalf("MarkScheduledTweetFailed: %v", err)
	}
	if err := s.CancelScheduledTweet(id); err != nil {
		t.Fatalf("CancelScheduledTweet: %v", err)
	}
}

func TestStore_ScheduledToolJobs(t *testing.T) {
	s := testStore(t)

	now := time.Now().UTC()
	id, err := s.CreateScheduledToolJob("sms_send", map[string]any{"text": "hello"}, now.Add(2*time.Minute), "once")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	items, err := s.ListScheduledToolJobs(10)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected scheduled tool jobs in list")
	}
	if items[0].ToolName == "" {
		t.Fatal("expected tool_name to be populated")
	}

	due, err := s.DueScheduledToolJobs(now.Add(5*time.Minute), 10)
	if err != nil {
		t.Fatalf("DueScheduledToolJobs: %v", err)
	}
	if len(due) == 0 {
		t.Fatal("expected due scheduled tool jobs")
	}

	if err := s.MarkScheduledToolJobSucceeded(id, "ok", now); err != nil {
		t.Fatalf("MarkScheduledToolJobSucceeded: %v", err)
	}
	if err := s.RescheduleScheduledToolJob(id, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("RescheduleScheduledToolJob: %v", err)
	}
	if err := s.MarkScheduledToolJobFailed(id, "test failure", "err", now); err != nil {
		t.Fatalf("MarkScheduledToolJobFailed: %v", err)
	}
	if err := s.CancelScheduledToolJob(id); err != nil {
		t.Fatalf("CancelScheduledToolJob: %v", err)
	}
}

func TestStore_UpdateScheduledToolJob(t *testing.T) {
	s := testStore(t)

	now := time.Now().UTC()
	id, err := s.CreateScheduledToolJob("sms_send", map[string]any{"text": "original"}, now.Add(2*time.Minute), "once")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}

	t.Run("updates tool input", func(t *testing.T) {
		newInput := map[string]any{"text": "updated tweet"}
		if err := s.UpdateScheduledToolJob(id, newInput, nil, nil); err != nil {
			t.Fatalf("UpdateScheduledToolJob: %v", err)
		}
		items, err := s.ListScheduledToolJobs(10)
		if err != nil {
			t.Fatalf("ListScheduledToolJobs: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("expected at least one job")
		}
		got, _ := items[0].ToolInput["text"].(string)
		if got != "updated tweet" {
			t.Errorf("text = %q, want %q", got, "updated tweet")
		}
	})

	t.Run("updates scheduled time", func(t *testing.T) {
		newTime := now.Add(1 * time.Hour)
		if err := s.UpdateScheduledToolJob(id, nil, &newTime, nil); err != nil {
			t.Fatalf("UpdateScheduledToolJob: %v", err)
		}
		items, err := s.ListScheduledToolJobs(10)
		if err != nil {
			t.Fatalf("ListScheduledToolJobs: %v", err)
		}
		if items[0].ScheduledFor.Before(now.Add(50 * time.Minute)) {
			t.Errorf("scheduled_for not updated: %s", items[0].ScheduledFor)
		}
	})

	t.Run("updates recurrence", func(t *testing.T) {
		rec := "daily"
		if err := s.UpdateScheduledToolJob(id, nil, nil, &rec); err != nil {
			t.Fatalf("UpdateScheduledToolJob: %v", err)
		}
		items, err := s.ListScheduledToolJobs(10)
		if err != nil {
			t.Fatalf("ListScheduledToolJobs: %v", err)
		}
		if items[0].Recurrence != "daily" {
			t.Errorf("recurrence = %q, want %q", items[0].Recurrence, "daily")
		}
	})

	t.Run("does not update non-pending jobs", func(t *testing.T) {
		// Cancel the job first
		if err := s.CancelScheduledToolJob(id); err != nil {
			t.Fatalf("CancelScheduledToolJob: %v", err)
		}
		newInput := map[string]any{"text": "should not work"}
		if err := s.UpdateScheduledToolJob(id, newInput, nil, nil); err != nil {
			t.Fatalf("UpdateScheduledToolJob: %v", err)
		}
		items, err := s.ListScheduledToolJobs(10)
		if err != nil {
			t.Fatalf("ListScheduledToolJobs: %v", err)
		}
		got, _ := items[0].ToolInput["text"].(string)
		if got == "should not work" {
			t.Error("should not have updated a cancelled job")
		}
	})

	t.Run("returns error with no fields", func(t *testing.T) {
		err := s.UpdateScheduledToolJob(id, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for no fields")
		}
	})
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func TestTruncateStoreText(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		limit  int
		expect string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"one over", "abcdef", 5, "abcde..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStoreText(tt.input, tt.limit)
			if got != tt.expect {
				t.Errorf("truncateStoreText(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.expect)
			}
		})
	}
}

func TestParseAnyTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC3339", "2024-06-15T10:30:00Z", false},
		{"SQLite datetime", "2024-06-15 10:30:00", false},
		{"invalid", "not-a-time", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAnyTime(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Year() != 2024 || result.Month() != 6 || result.Day() != 15 {
				t.Errorf("parsed time = %v", result)
			}
		})
	}
}

func TestParseOptionalTime(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{"valid RFC3339", "2024-06-15T10:30:00Z", true},
		{"valid SQLite", "2024-06-15 10:30:00", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"invalid", "garbage", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := parseOptionalTime(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseOptionalTime(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Default limit paths
// ---------------------------------------------------------------------------

func TestStore_ListScheduledToolJobs_defaultLimit(t *testing.T) {
	s := testStore(t)
	// With no jobs, zero limit should default to 50 and return empty.
	items, err := s.ListScheduledToolJobs(0)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs(0): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestStore_DueScheduledToolJobs_defaultLimit(t *testing.T) {
	s := testStore(t)
	items, err := s.DueScheduledToolJobs(time.Now(), 0)
	if err != nil {
		t.Fatalf("DueScheduledToolJobs(0): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestStore_ListScheduledTweets_defaultLimit(t *testing.T) {
	s := testStore(t)
	items, err := s.ListScheduledTweets(0)
	if err != nil {
		t.Fatalf("ListScheduledTweets(0): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestStore_DueScheduledTweets_defaultLimit(t *testing.T) {
	s := testStore(t)
	items, err := s.DueScheduledTweets(time.Now(), 0)
	if err != nil {
		t.Fatalf("DueScheduledTweets(0): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Default recurrence
// ---------------------------------------------------------------------------

func TestStore_CreateScheduledToolJob_emptyRecurrence(t *testing.T) {
	s := testStore(t)
	id, err := s.CreateScheduledToolJob("test_tool", map[string]any{"key": "val"}, time.Now().Add(time.Hour), "")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}
	items, err := s.ListScheduledToolJobs(10)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs: %v", err)
	}
	var found *ScheduledToolJob
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("job not found")
	}
	if found.Recurrence != "once" {
		t.Errorf("Recurrence = %q, want %q", found.Recurrence, "once")
	}
}

func TestStore_CreateScheduledTweet_emptyRecurrence(t *testing.T) {
	s := testStore(t)
	id, err := s.CreateScheduledTweet("tweet text", time.Now().Add(time.Hour), "")
	if err != nil {
		t.Fatalf("CreateScheduledTweet: %v", err)
	}
	items, err := s.ListScheduledTweets(10)
	if err != nil {
		t.Fatalf("ListScheduledTweets: %v", err)
	}
	var found *ScheduledTweet
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("tweet not found")
	}
	if found.Recurrence != "once" {
		t.Errorf("Recurrence = %q, want %q", found.Recurrence, "once")
	}
}

// ---------------------------------------------------------------------------
// Truncation in scheduled tool jobs
// ---------------------------------------------------------------------------

func TestStore_MarkScheduledToolJobSucceeded_truncatesResult(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	id, err := s.CreateScheduledToolJob("tool", map[string]any{}, now.Add(-time.Minute), "once")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}

	longResult := strings.Repeat("x", 5000)
	if err := s.MarkScheduledToolJobSucceeded(id, longResult, now); err != nil {
		t.Fatalf("MarkScheduledToolJobSucceeded: %v", err)
	}

	items, err := s.ListScheduledToolJobs(10)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs: %v", err)
	}
	for _, item := range items {
		if item.ID == id {
			if len(item.LastResult) > 4010 {
				t.Errorf("result not truncated: len = %d", len(item.LastResult))
			}
			return
		}
	}
	t.Fatal("job not found after marking succeeded")
}

func TestStore_MarkScheduledToolJobFailed_truncatesError(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	id, err := s.CreateScheduledToolJob("tool", map[string]any{}, now.Add(-time.Minute), "once")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}

	longErr := strings.Repeat("e", 3000)
	longResult := strings.Repeat("r", 5000)
	if err := s.MarkScheduledToolJobFailed(id, longErr, longResult, now); err != nil {
		t.Fatalf("MarkScheduledToolJobFailed: %v", err)
	}

	items, err := s.ListScheduledToolJobs(10)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs: %v", err)
	}
	for _, item := range items {
		if item.ID == id {
			if len(item.LastError) > 2010 {
				t.Errorf("error not truncated: len = %d", len(item.LastError))
			}
			if len(item.LastResult) > 4010 {
				t.Errorf("result not truncated: len = %d", len(item.LastResult))
			}
			return
		}
	}
	t.Fatal("job not found")
}

// ---------------------------------------------------------------------------
// Scheduled tool jobs — optional time fields populated
// ---------------------------------------------------------------------------

func TestStore_ScheduledToolJob_optionalTimesParsed(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()

	id, err := s.CreateScheduledToolJob("tool", map[string]any{"k": "v"}, now.Add(-time.Minute), "daily")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}

	// Mark succeeded to populate last_attempt_at and completed_at.
	if err := s.MarkScheduledToolJobSucceeded(id, "done", now); err != nil {
		t.Fatalf("MarkScheduledToolJobSucceeded: %v", err)
	}

	items, err := s.ListScheduledToolJobs(10)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs: %v", err)
	}
	for _, item := range items {
		if item.ID == id {
			if item.LastAttemptAt == nil {
				t.Error("expected LastAttemptAt to be populated")
			}
			if item.CompletedAt == nil {
				t.Error("expected CompletedAt to be populated")
			}
			if item.AttemptCount != 1 {
				t.Errorf("AttemptCount = %d, want 1", item.AttemptCount)
			}
			if item.Status != "completed" {
				t.Errorf("Status = %q, want completed", item.Status)
			}
			return
		}
	}
	t.Fatal("job not found")
}

// ---------------------------------------------------------------------------
// Scheduled tweets — optional time fields populated
// ---------------------------------------------------------------------------

func TestStore_ScheduledTweet_optionalTimesParsed(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()

	id, err := s.CreateScheduledTweet("hello", now.Add(-time.Minute), "once")
	if err != nil {
		t.Fatalf("CreateScheduledTweet: %v", err)
	}

	// Mark posted to populate last_attempt_at and posted_at.
	if err := s.MarkScheduledTweetPosted(id, "tweet_123", now); err != nil {
		t.Fatalf("MarkScheduledTweetPosted: %v", err)
	}

	items, err := s.ListScheduledTweets(10)
	if err != nil {
		t.Fatalf("ListScheduledTweets: %v", err)
	}
	for _, item := range items {
		if item.ID == id {
			if item.LastAttemptAt == nil {
				t.Error("expected LastAttemptAt to be populated")
			}
			if item.PostedAt == nil {
				t.Error("expected PostedAt to be populated")
			}
			if item.TweetID != "tweet_123" {
				t.Errorf("TweetID = %q, want tweet_123", item.TweetID)
			}
			if item.Status != "posted" {
				t.Errorf("Status = %q, want posted", item.Status)
			}
			return
		}
	}
	t.Fatal("tweet not found")
}

// ---------------------------------------------------------------------------
// GetMessagesAfterSequence with blocks
// ---------------------------------------------------------------------------

func TestStore_GetMessagesAfterSequence_withBlocks(t *testing.T) {
	s := testStore(t)
	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Append plain text first.
	if err := s.AppendMessage(sess.ID, "user", "hello", 0); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	// Append block message at sequence 2.
	blocks := []domain.ContentBlock{
		{Type: "text", Text: "response"},
		{Type: "tool_use", ToolUseID: "t1", ToolName: "bash"},
	}
	if err := s.AppendMessageBlocks(sess.ID, "assistant", blocks, 0); err != nil {
		t.Fatalf("AppendMessageBlocks: %v", err)
	}

	msgs, err := s.GetMessagesAfterSequence(sess.ID, 1)
	if err != nil {
		t.Fatalf("GetMessagesAfterSequence: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].HasBlocks() {
		t.Error("expected blocks to be populated")
	}
	if len(msgs[0].Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(msgs[0].Blocks))
	}
	if msgs[0].Content != "response" {
		t.Errorf("Content = %q, want %q", msgs[0].Content, "response")
	}
}

// ---------------------------------------------------------------------------
// SessionTitle helper
// ---------------------------------------------------------------------------

func TestStore_SessionTitle_defaultTitle(t *testing.T) {
	s := testStore(t)
	sess, err := s.CreateSession("/tmp", "model")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Before any title update, should return "New Session".
	got := s.SessionTitle(sess.ID)
	if got != "New Session" {
		t.Errorf("SessionTitle = %q, want %q", got, "New Session")
	}
}

// ---------------------------------------------------------------------------
// Tool name normalization
// ---------------------------------------------------------------------------

func TestStore_CreateScheduledToolJob_normalizesName(t *testing.T) {
	s := testStore(t)
	id, err := s.CreateScheduledToolJob("  SMS_SEND  ", map[string]any{}, time.Now().Add(time.Hour), "once")
	if err != nil {
		t.Fatalf("CreateScheduledToolJob: %v", err)
	}
	items, err := s.ListScheduledToolJobs(10)
	if err != nil {
		t.Fatalf("ListScheduledToolJobs: %v", err)
	}
	for _, item := range items {
		if item.ID == id {
			if item.ToolName != "sms_send" {
				t.Errorf("ToolName = %q, want %q", item.ToolName, "sms_send")
			}
			return
		}
	}
	t.Fatal("job not found")
}

// ---------------------------------------------------------------------------
// ListSessions with negative limit
// ---------------------------------------------------------------------------

func TestStore_ListSessions_negativeLimit(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateSession("/proj", "model"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Negative limit should default to 10.
	sessions, err := s.ListSessions("/proj", -1)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}
