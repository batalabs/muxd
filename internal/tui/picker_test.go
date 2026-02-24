package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/batalabs/muxd/internal/domain"
)

func testSessions() []domain.Session {
	return []domain.Session{
		{ID: "aaaaaaaa-1111-2222-3333-444444444444", Title: "Fix login bug", Tags: "bugfix", MessageCount: 5, UpdatedAt: time.Now()},
		{ID: "bbbbbbbb-1111-2222-3333-444444444444", Title: "Add user auth", Tags: "feature", MessageCount: 12, UpdatedAt: time.Now().Add(-time.Hour)},
		{ID: "cccccccc-1111-2222-3333-444444444444", Title: "Refactor database", Tags: "refactor", MessageCount: 8, UpdatedAt: time.Now().Add(-24 * time.Hour)},
	}
}

func TestSessionPicker_IsActive(t *testing.T) {
	t.Run("nil picker is not active", func(t *testing.T) {
		var p *SessionPicker
		if p.IsActive() {
			t.Error("expected nil picker to not be active")
		}
	})

	t.Run("new picker is active", func(t *testing.T) {
		p := NewSessionPicker(testSessions())
		if !p.IsActive() {
			t.Error("expected new picker to be active")
		}
	})
}

func TestSessionPicker_Dismiss(t *testing.T) {
	p := NewSessionPicker(testSessions())
	p.Dismiss()
	if p.IsActive() {
		t.Error("expected picker to be inactive after dismiss")
	}
}

func TestSessionPicker_EmptyFilterShowsAll(t *testing.T) {
	sessions := testSessions()
	p := NewSessionPicker(sessions)
	if len(p.filtered) != len(sessions) {
		t.Errorf("filtered = %d, want %d", len(p.filtered), len(sessions))
	}
}

func TestSessionPicker_FilterNarrowsResults(t *testing.T) {
	p := NewSessionPicker(testSessions())

	t.Run("filter by title", func(t *testing.T) {
		p.SetFilter("login")
		if len(p.filtered) != 1 {
			t.Errorf("filtered = %d, want 1", len(p.filtered))
		}
		if p.filtered[0].Title != "Fix login bug" {
			t.Errorf("filtered[0].Title = %q, want %q", p.filtered[0].Title, "Fix login bug")
		}
	})

	t.Run("filter by tag", func(t *testing.T) {
		p.SetFilter("refactor")
		if len(p.filtered) != 1 {
			t.Errorf("filtered = %d, want 1", len(p.filtered))
		}
	})

	t.Run("filter by ID prefix", func(t *testing.T) {
		p.SetFilter("bbbb")
		if len(p.filtered) != 1 {
			t.Errorf("filtered = %d, want 1", len(p.filtered))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		p.SetFilter("zzzzzzz")
		if len(p.filtered) != 0 {
			t.Errorf("filtered = %d, want 0", len(p.filtered))
		}
	})
}

func TestSessionPicker_Navigation(t *testing.T) {
	p := NewSessionPicker(testSessions())

	t.Run("starts at index 0", func(t *testing.T) {
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", p.selectedIdx)
		}
	})

	t.Run("move down", func(t *testing.T) {
		p.MoveDown()
		if p.selectedIdx != 1 {
			t.Errorf("selectedIdx = %d, want 1", p.selectedIdx)
		}
	})

	t.Run("move down at end stays", func(t *testing.T) {
		p.MoveDown() // 2
		p.MoveDown() // still 2
		if p.selectedIdx != 2 {
			t.Errorf("selectedIdx = %d, want 2", p.selectedIdx)
		}
	})

	t.Run("move up", func(t *testing.T) {
		p.MoveUp()
		if p.selectedIdx != 1 {
			t.Errorf("selectedIdx = %d, want 1", p.selectedIdx)
		}
	})

	t.Run("move up at top stays", func(t *testing.T) {
		p.MoveUp() // 0
		p.MoveUp() // still 0
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", p.selectedIdx)
		}
	})
}

func TestSessionPicker_SelectedSession(t *testing.T) {
	sessions := testSessions()
	p := NewSessionPicker(sessions)

	t.Run("returns first session initially", func(t *testing.T) {
		sel := p.SelectedSession()
		if sel == nil {
			t.Fatal("expected non-nil selected session")
		}
		if sel.ID != sessions[0].ID {
			t.Errorf("selected ID = %q, want %q", sel.ID, sessions[0].ID)
		}
	})

	t.Run("returns nil when filtered to empty", func(t *testing.T) {
		p.SetFilter("zzzzzzz")
		sel := p.SelectedSession()
		if sel != nil {
			t.Error("expected nil selected session")
		}
	})
}

func TestSessionPicker_AppendAndBackspaceFilter(t *testing.T) {
	p := NewSessionPicker(testSessions())

	p.AppendFilter('l')
	p.AppendFilter('o')
	p.AppendFilter('g')
	if p.filter != "log" {
		t.Errorf("filter = %q, want %q", p.filter, "log")
	}
	if len(p.filtered) != 1 {
		t.Errorf("filtered = %d, want 1", len(p.filtered))
	}

	p.BackspaceFilter()
	p.BackspaceFilter()
	p.BackspaceFilter()
	if p.filter != "" {
		t.Errorf("filter = %q, want empty", p.filter)
	}
	if len(p.filtered) != 3 {
		t.Errorf("filtered = %d, want 3", len(p.filtered))
	}

	// Backspace on empty is a no-op
	p.BackspaceFilter()
	if p.filter != "" {
		t.Errorf("filter = %q, want empty", p.filter)
	}
}

func TestSessionPicker_ViewContainsIDsAndTitles(t *testing.T) {
	sessions := testSessions()
	p := NewSessionPicker(sessions)
	view := p.View(80)

	for _, s := range sessions {
		if !strings.Contains(view, s.ID[:8]) {
			t.Errorf("view should contain ID prefix %q", s.ID[:8])
		}
		if !strings.Contains(view, s.Title) {
			t.Errorf("view should contain title %q", s.Title)
		}
	}

	if !strings.Contains(view, "Enter=open") {
		t.Error("view should contain footer hint")
	}
	if !strings.Contains(view, "r=rename") {
		t.Error("view should contain rename hint")
	}
	if !strings.Contains(view, "d=delete") {
		t.Error("view should contain delete hint")
	}
	if !strings.Contains(view, "Space=select") {
		t.Error("view should contain Space=select hint")
	}
	if !strings.Contains(view, "a=all") {
		t.Error("view should contain a=all hint")
	}
}

func TestSessionPicker_Rename(t *testing.T) {
	p := NewSessionPicker(testSessions())

	t.Run("starts in browse mode", func(t *testing.T) {
		if p.Mode() != pickerBrowse {
			t.Errorf("mode = %d, want pickerBrowse", p.Mode())
		}
	})

	t.Run("start rename pre-fills title", func(t *testing.T) {
		p.StartRename()
		if p.Mode() != pickerRenaming {
			t.Errorf("mode = %d, want pickerRenaming", p.Mode())
		}
		if p.RenameBuffer() != "Fix login bug" {
			t.Errorf("renameBuf = %q, want %q", p.RenameBuffer(), "Fix login bug")
		}
	})

	t.Run("append and backspace rename", func(t *testing.T) {
		// Clear existing title and type new one
		for range p.RenameBuffer() {
			p.BackspaceRename()
		}
		p.AppendRename('N')
		p.AppendRename('e')
		p.AppendRename('w')
		if p.RenameBuffer() != "New" {
			t.Errorf("renameBuf = %q, want %q", p.RenameBuffer(), "New")
		}
	})

	t.Run("commit rename updates session", func(t *testing.T) {
		id, title := p.CommitRename()
		if id == "" {
			t.Fatal("expected non-empty ID")
		}
		if title != "New" {
			t.Errorf("title = %q, want %q", title, "New")
		}
		if p.Mode() != pickerBrowse {
			t.Errorf("mode = %d, want pickerBrowse", p.Mode())
		}
		// Verify the session was updated in the list
		sel := p.SelectedSession()
		if sel.Title != "New" {
			t.Errorf("selected title = %q, want %q", sel.Title, "New")
		}
	})

	t.Run("cancel rename returns to browse", func(t *testing.T) {
		p.StartRename()
		p.CancelMode()
		if p.Mode() != pickerBrowse {
			t.Errorf("mode = %d, want pickerBrowse", p.Mode())
		}
	})

	t.Run("commit empty rename is no-op", func(t *testing.T) {
		p.StartRename()
		for range p.RenameBuffer() {
			p.BackspaceRename()
		}
		id, _ := p.CommitRename()
		if id != "" {
			t.Error("expected empty ID for empty rename")
		}
	})
}

func TestSessionPicker_Delete(t *testing.T) {
	p := NewSessionPicker(testSessions())

	t.Run("start delete enters confirm mode", func(t *testing.T) {
		p.StartDelete()
		if p.Mode() != pickerConfirmDelete {
			t.Errorf("mode = %d, want pickerConfirmDelete", p.Mode())
		}
	})

	t.Run("cancel delete returns to browse", func(t *testing.T) {
		p.CancelMode()
		if p.Mode() != pickerBrowse {
			t.Errorf("mode = %d, want pickerBrowse", p.Mode())
		}
	})

	t.Run("remove selected removes from list", func(t *testing.T) {
		before := len(p.filtered)
		p.StartDelete()
		id := p.RemoveSelected()
		if id == "" {
			t.Fatal("expected non-empty ID")
		}
		if len(p.filtered) != before-1 {
			t.Errorf("filtered = %d, want %d", len(p.filtered), before-1)
		}
		if p.Mode() != pickerBrowse {
			t.Errorf("mode = %d, want pickerBrowse", p.Mode())
		}
	})

	t.Run("delete adjusts selection index", func(t *testing.T) {
		// Move to last item and delete it
		for i := 0; i < len(p.filtered); i++ {
			p.MoveDown()
		}
		lastIdx := p.selectedIdx
		p.StartDelete()
		p.RemoveSelected()
		if p.selectedIdx > lastIdx {
			t.Errorf("selectedIdx = %d, should not exceed %d", p.selectedIdx, lastIdx)
		}
	})
}

func TestSessionPicker_ViewRenameMode(t *testing.T) {
	p := NewSessionPicker(testSessions())
	p.StartRename()
	view := p.View(80)

	if !strings.Contains(view, "Rename:") {
		t.Error("rename view should contain 'Rename:'")
	}
	if !strings.Contains(view, "Enter=save") {
		t.Error("rename view should show save hint")
	}
}

func TestSessionPicker_ViewDeleteMode(t *testing.T) {
	p := NewSessionPicker(testSessions())
	p.StartDelete()
	view := p.View(80)

	if !strings.Contains(view, "Delete") {
		t.Error("delete view should contain 'Delete'")
	}
	if !strings.Contains(view, "y=delete") {
		t.Error("delete view should show confirm hint")
	}
}

func TestSessionPicker_ToggleSelected(t *testing.T) {
	p := NewSessionPicker(testSessions())

	t.Run("toggle on", func(t *testing.T) {
		p.ToggleSelected()
		if p.SelectedCount() != 1 {
			t.Errorf("SelectedCount = %d, want 1", p.SelectedCount())
		}
	})

	t.Run("toggle off", func(t *testing.T) {
		p.ToggleSelected()
		if p.SelectedCount() != 0 {
			t.Errorf("SelectedCount = %d, want 0", p.SelectedCount())
		}
	})

	t.Run("toggle on empty list is no-op", func(t *testing.T) {
		p.SetFilter("zzzzzzz")
		p.ToggleSelected()
		if p.SelectedCount() != 0 {
			t.Errorf("SelectedCount = %d, want 0", p.SelectedCount())
		}
		p.SetFilter("")
	})
}

func TestSessionPicker_SelectAll(t *testing.T) {
	p := NewSessionPicker(testSessions())

	t.Run("select all", func(t *testing.T) {
		p.SelectAll()
		if p.SelectedCount() != 3 {
			t.Errorf("SelectedCount = %d, want 3", p.SelectedCount())
		}
	})

	t.Run("toggle deselects all when all selected", func(t *testing.T) {
		p.SelectAll() // all already selected → deselect
		if p.SelectedCount() != 0 {
			t.Errorf("SelectedCount = %d, want 0", p.SelectedCount())
		}
	})

	t.Run("select all with partial selection selects remaining", func(t *testing.T) {
		p.ToggleSelected() // select first
		p.SelectAll()      // should select all
		if p.SelectedCount() != 3 {
			t.Errorf("SelectedCount = %d, want 3", p.SelectedCount())
		}
		p.ClearSelected()
	})
}

func TestSessionPicker_SelectedIDs(t *testing.T) {
	sessions := testSessions()
	p := NewSessionPicker(sessions)

	p.ToggleSelected() // select first (index 0)
	p.MoveDown()
	p.ToggleSelected() // select second (index 1)

	ids := p.SelectedIDs()
	if len(ids) != 2 {
		t.Fatalf("SelectedIDs len = %d, want 2", len(ids))
	}

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet[sessions[0].ID] {
		t.Errorf("expected %s in SelectedIDs", sessions[0].ID)
	}
	if !idSet[sessions[1].ID] {
		t.Errorf("expected %s in SelectedIDs", sessions[1].ID)
	}
}

func TestSessionPicker_ClearSelected(t *testing.T) {
	p := NewSessionPicker(testSessions())
	p.SelectAll()
	p.ClearSelected()
	if p.SelectedCount() != 0 {
		t.Errorf("SelectedCount = %d, want 0", p.SelectedCount())
	}
}

func TestSessionPicker_RemoveSelectedMulti(t *testing.T) {
	t.Run("removes multiple sessions", func(t *testing.T) {
		sessions := testSessions()
		p := NewSessionPicker(sessions)

		p.ToggleSelected() // select first
		p.MoveDown()
		p.ToggleSelected() // select second

		removed := p.RemoveSelectedMulti()
		if len(removed) != 2 {
			t.Fatalf("removed len = %d, want 2", len(removed))
		}
		if len(p.filtered) != 1 {
			t.Errorf("filtered = %d, want 1", len(p.filtered))
		}
		if p.SelectedCount() != 0 {
			t.Errorf("SelectedCount = %d, want 0 after remove", p.SelectedCount())
		}
		if p.Mode() != pickerBrowse {
			t.Errorf("mode = %d, want pickerBrowse", p.Mode())
		}
	})

	t.Run("returns nil when nothing selected", func(t *testing.T) {
		p := NewSessionPicker(testSessions())
		removed := p.RemoveSelectedMulti()
		if removed != nil {
			t.Errorf("expected nil, got %v", removed)
		}
	})

	t.Run("adjusts index when at end", func(t *testing.T) {
		sessions := testSessions()
		p := NewSessionPicker(sessions)

		// Move to last and select it
		p.MoveDown()
		p.MoveDown()
		p.ToggleSelected()

		p.RemoveSelectedMulti()
		if p.selectedIdx >= len(p.filtered) {
			t.Errorf("selectedIdx = %d, should be < %d", p.selectedIdx, len(p.filtered))
		}
	})

	t.Run("removes all sessions", func(t *testing.T) {
		p := NewSessionPicker(testSessions())
		p.SelectAll()
		removed := p.RemoveSelectedMulti()
		if len(removed) != 3 {
			t.Fatalf("removed len = %d, want 3", len(removed))
		}
		if len(p.filtered) != 0 {
			t.Errorf("filtered = %d, want 0", len(p.filtered))
		}
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", p.selectedIdx)
		}
	})
}

func TestSessionPicker_ViewMultiSelectMarkers(t *testing.T) {
	p := NewSessionPicker(testSessions())
	p.ToggleSelected() // select first

	view := p.View(80)
	if !strings.Contains(view, "[x]") {
		t.Error("view should contain [x] for selected session")
	}
	if !strings.Contains(view, "[ ]") {
		t.Error("view should contain [ ] for unselected sessions")
	}
}

func TestSessionPicker_ViewMultiDeleteConfirm(t *testing.T) {
	p := NewSessionPicker(testSessions())
	p.SelectAll()
	p.StartDelete()
	view := p.View(80)

	if !strings.Contains(view, "Delete 3 sessions?") {
		t.Error("multi-delete view should show count: 'Delete 3 sessions?'")
	}
}

func TestSessionPicker_ViewSingleDeleteConfirm(t *testing.T) {
	p := NewSessionPicker(testSessions())
	// No multi-select, just start delete on highlighted
	p.StartDelete()
	view := p.View(80)

	if !strings.Contains(view, `Delete "Fix login bug"?`) {
		t.Error("single-delete view should show session title")
	}
}
