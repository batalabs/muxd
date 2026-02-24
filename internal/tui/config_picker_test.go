package tui

import (
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/config"
)

func testPrefs() config.Preferences {
	return config.Preferences{
		Model:         "claude-sonnet",
		FooterTokens:  true,
		FooterSession: false,
	}
}

func TestConfigPicker_IsActive(t *testing.T) {
	t.Run("nil picker is not active", func(t *testing.T) {
		var p *ConfigPicker
		if p.IsActive() {
			t.Error("expected nil picker to not be active")
		}
	})

	t.Run("new picker is active", func(t *testing.T) {
		p := NewConfigPicker(testPrefs())
		if !p.IsActive() {
			t.Error("expected new picker to be active")
		}
	})
}

func TestConfigPicker_Dismiss(t *testing.T) {
	p := NewConfigPicker(testPrefs())
	p.Dismiss()
	if p.IsActive() {
		t.Error("expected picker to be inactive after dismiss")
	}
}

func TestConfigPicker_GroupNavigation(t *testing.T) {
	p := NewConfigPicker(testPrefs())
	if len(p.groups) == 0 {
		t.Fatal("expected at least one config group")
	}

	t.Run("starts in groups mode at index 0", func(t *testing.T) {
		if p.mode != configPickerGroups {
			t.Errorf("mode = %d, want configPickerGroups", p.mode)
		}
		if p.groupIdx != 0 {
			t.Errorf("groupIdx = %d, want 0", p.groupIdx)
		}
	})

	t.Run("move down increments group index", func(t *testing.T) {
		if len(p.groups) > 1 {
			p.MoveDown()
			if p.groupIdx != 1 {
				t.Errorf("groupIdx = %d, want 1", p.groupIdx)
			}
			p.MoveUp()
		}
	})

	t.Run("move up at top stays at 0", func(t *testing.T) {
		p.groupIdx = 0
		p.MoveUp()
		if p.groupIdx != 0 {
			t.Errorf("groupIdx = %d, want 0", p.groupIdx)
		}
	})

	t.Run("move down at bottom stays at last", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			p.MoveDown()
		}
		want := len(p.groups) - 1
		if p.groupIdx != want {
			t.Errorf("groupIdx = %d, want %d", p.groupIdx, want)
		}
	})
}

func TestConfigPicker_EnterGroup(t *testing.T) {
	p := NewConfigPicker(testPrefs())
	p.EnterGroup()

	if p.mode != configPickerKeys {
		t.Errorf("mode = %d, want configPickerKeys", p.mode)
	}
	if p.keyIdx != 0 {
		t.Errorf("keyIdx = %d, want 0 after entering group", p.keyIdx)
	}
}

func TestConfigPicker_Back(t *testing.T) {
	p := NewConfigPicker(testPrefs())

	t.Run("back from keys returns to groups", func(t *testing.T) {
		p.EnterGroup()
		p.Back()
		if p.mode != configPickerGroups {
			t.Errorf("mode = %d, want configPickerGroups", p.mode)
		}
	})

	t.Run("back from edit returns to keys", func(t *testing.T) {
		p.EnterGroup()
		p.StartEdit("model", "claude-sonnet")
		if p.mode != configPickerEdit {
			t.Fatal("expected configPickerEdit mode")
		}
		p.Back()
		if p.mode != configPickerKeys {
			t.Errorf("mode = %d, want configPickerKeys", p.mode)
		}
		if p.editKey != "" || p.editBuf != "" {
			t.Error("expected edit state to be cleared after back")
		}
	})
}

func TestConfigPicker_KeyNavigation(t *testing.T) {
	p := NewConfigPicker(testPrefs())
	p.EnterGroup()
	g := p.selectedGroup()
	if g == nil || len(g.Entries) == 0 {
		t.Skip("no entries in first group")
	}

	t.Run("starts at key index 0", func(t *testing.T) {
		if p.keyIdx != 0 {
			t.Errorf("keyIdx = %d, want 0", p.keyIdx)
		}
	})

	t.Run("move down increments key index", func(t *testing.T) {
		if len(g.Entries) > 1 {
			p.MoveDown()
			if p.keyIdx != 1 {
				t.Errorf("keyIdx = %d, want 1", p.keyIdx)
			}
			p.MoveUp()
		}
	})

	t.Run("move up at top stays at 0", func(t *testing.T) {
		p.keyIdx = 0
		p.MoveUp()
		if p.keyIdx != 0 {
			t.Errorf("keyIdx = %d, want 0", p.keyIdx)
		}
	})
}

func TestConfigPicker_EditLifecycle(t *testing.T) {
	p := NewConfigPicker(testPrefs())
	p.EnterGroup()

	t.Run("start edit sets mode and buffer", func(t *testing.T) {
		p.StartEdit("model", "old-value")
		if p.mode != configPickerEdit {
			t.Errorf("mode = %d, want configPickerEdit", p.mode)
		}
		if p.editKey != "model" {
			t.Errorf("editKey = %q, want %q", p.editKey, "model")
		}
		if p.editBuf != "old-value" {
			t.Errorf("editBuf = %q, want %q", p.editBuf, "old-value")
		}
	})

	t.Run("append edit adds character", func(t *testing.T) {
		p.AppendEdit('!')
		if p.editBuf != "old-value!" {
			t.Errorf("editBuf = %q, want %q", p.editBuf, "old-value!")
		}
	})

	t.Run("backspace edit removes character", func(t *testing.T) {
		p.BackspaceEdit()
		if p.editBuf != "old-value" {
			t.Errorf("editBuf = %q, want %q", p.editBuf, "old-value")
		}
	})

	t.Run("backspace on empty is no-op", func(t *testing.T) {
		p.editBuf = ""
		p.BackspaceEdit()
		if p.editBuf != "" {
			t.Errorf("editBuf = %q, want empty", p.editBuf)
		}
	})

	t.Run("commit returns key and value", func(t *testing.T) {
		p.editBuf = "new-value"
		key, value, ok := p.CommitEdit()
		if !ok {
			t.Fatal("expected ok = true")
		}
		if key != "model" {
			t.Errorf("key = %q, want %q", key, "model")
		}
		if value != "new-value" {
			t.Errorf("value = %q, want %q", value, "new-value")
		}
		if p.mode != configPickerKeys {
			t.Errorf("mode = %d, want configPickerKeys after commit", p.mode)
		}
		if p.editKey != "" || p.editBuf != "" {
			t.Error("expected edit state cleared after commit")
		}
	})

	t.Run("commit outside edit mode returns not ok", func(t *testing.T) {
		_, _, ok := p.CommitEdit()
		if ok {
			t.Error("expected ok = false when not in edit mode")
		}
	})
}

func TestConfigPicker_FocusGroup(t *testing.T) {
	p := NewConfigPicker(testPrefs())

	t.Run("focus known group switches mode", func(t *testing.T) {
		p.FocusGroup("models")
		if p.mode != configPickerKeys {
			t.Errorf("mode = %d, want configPickerKeys", p.mode)
		}
		g := p.selectedGroup()
		if g == nil || g.Name != "models" {
			t.Error("expected selected group to be 'models'")
		}
	})

	t.Run("focus unknown group is no-op", func(t *testing.T) {
		before := p.groupIdx
		p.FocusGroup("nonexistent")
		if p.groupIdx != before {
			t.Error("expected groupIdx unchanged for unknown group")
		}
	})
}

func TestConfigPicker_Refresh(t *testing.T) {
	p := NewConfigPicker(testPrefs())
	// Move to a high index then refresh with same prefs.
	p.groupIdx = 999
	p.Refresh(testPrefs())
	if p.groupIdx >= len(p.groups) {
		t.Errorf("groupIdx = %d, should be clamped to valid range", p.groupIdx)
	}
}

func TestConfigPicker_SelectedGroupNil(t *testing.T) {
	p := &ConfigPicker{active: true}
	if g := p.selectedGroup(); g != nil {
		t.Error("expected nil group when groups is empty")
	}
	if e := p.selectedEntry(); e != nil {
		t.Error("expected nil entry when groups is empty")
	}
}

func TestConfigPickerAtGroup(t *testing.T) {
	p := NewConfigPickerAtGroup(testPrefs(), "models")
	if p.mode != configPickerKeys {
		t.Errorf("mode = %d, want configPickerKeys", p.mode)
	}
	g := p.selectedGroup()
	if g == nil || g.Name != "models" {
		t.Error("expected selected group to be 'models'")
	}
}

func TestConfigPicker_View(t *testing.T) {
	p := NewConfigPicker(testPrefs())

	t.Run("groups view contains header", func(t *testing.T) {
		view := p.View(80)
		if !strings.Contains(view, "Config Picker") {
			t.Error("expected view to contain 'Config Picker'")
		}
		if !strings.Contains(view, "Enter=select group") {
			t.Error("expected groups mode help text")
		}
	})

	t.Run("keys view contains group name", func(t *testing.T) {
		p.EnterGroup()
		view := p.View(80)
		if !strings.Contains(view, "Group:") {
			t.Error("expected keys view to contain 'Group:'")
		}
		if !strings.Contains(view, "Enter=edit/toggle") {
			t.Error("expected keys mode help text")
		}
	})

	t.Run("edit view contains edit key", func(t *testing.T) {
		p.StartEdit("model", "test")
		view := p.View(80)
		if !strings.Contains(view, "Edit model") {
			t.Error("expected edit view to contain 'Edit model'")
		}
		if !strings.Contains(view, "Enter=save") {
			t.Error("expected edit mode help text")
		}
	})

	t.Run("minimum width enforced", func(t *testing.T) {
		p2 := NewConfigPicker(testPrefs())
		view := p2.View(10)
		if view == "" {
			t.Error("expected non-empty view with narrow width")
		}
	})
}
