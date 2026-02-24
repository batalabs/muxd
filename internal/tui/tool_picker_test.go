package tui

import (
	"strings"
	"testing"
)

func testToolNames() []string {
	return []string{"bash", "file_read", "file_write", "grep", "web_search"}
}

func TestToolPicker_IsActive(t *testing.T) {
	t.Run("nil picker is not active", func(t *testing.T) {
		var p *ToolPicker
		if p.IsActive() {
			t.Error("expected nil picker to not be active")
		}
	})

	t.Run("new picker is active", func(t *testing.T) {
		p := NewToolPicker(testToolNames(), nil)
		if !p.IsActive() {
			t.Error("expected new picker to be active")
		}
	})
}

func TestToolPicker_Dismiss(t *testing.T) {
	p := NewToolPicker(testToolNames(), nil)
	p.Dismiss()
	if p.IsActive() {
		t.Error("expected picker to be inactive after dismiss")
	}
}

func TestToolPicker_NamesAreSorted(t *testing.T) {
	p := NewToolPicker([]string{"grep", "bash", "file_read"}, nil)
	for i := 1; i < len(p.names); i++ {
		if p.names[i] < p.names[i-1] {
			t.Errorf("names not sorted: %q before %q", p.names[i-1], p.names[i])
		}
	}
}

func TestToolPicker_DisabledMapCopied(t *testing.T) {
	orig := map[string]bool{"bash": true}
	p := NewToolPicker(testToolNames(), orig)

	// Mutating original should not affect picker.
	orig["file_read"] = true
	if p.disabled["file_read"] {
		t.Error("expected picker disabled map to be independent of original")
	}
}

func TestToolPicker_Navigation(t *testing.T) {
	p := NewToolPicker(testToolNames(), nil)

	t.Run("starts at index 0", func(t *testing.T) {
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", p.selectedIdx)
		}
	})

	t.Run("move down increments", func(t *testing.T) {
		p.MoveDown()
		if p.selectedIdx != 1 {
			t.Errorf("selectedIdx = %d, want 1", p.selectedIdx)
		}
	})

	t.Run("move up decrements", func(t *testing.T) {
		p.MoveUp()
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", p.selectedIdx)
		}
	})

	t.Run("move up at top stays at 0", func(t *testing.T) {
		p.MoveUp()
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", p.selectedIdx)
		}
	})

	t.Run("move down at bottom stays at last", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			p.MoveDown()
		}
		want := len(p.filtered) - 1
		if p.selectedIdx != want {
			t.Errorf("selectedIdx = %d, want %d", p.selectedIdx, want)
		}
	})
}

func TestToolPicker_SelectedName(t *testing.T) {
	t.Run("returns first sorted name", func(t *testing.T) {
		p := NewToolPicker(testToolNames(), nil)
		got := p.SelectedName()
		if got != "bash" {
			t.Errorf("SelectedName() = %q, want %q", got, "bash")
		}
	})

	t.Run("empty picker returns empty string", func(t *testing.T) {
		p := NewToolPicker(nil, nil)
		if got := p.SelectedName(); got != "" {
			t.Errorf("SelectedName() = %q, want empty", got)
		}
	})
}

func TestToolPicker_ToggleSelected(t *testing.T) {
	p := NewToolPicker(testToolNames(), nil)
	name := p.SelectedName()

	t.Run("toggle enables disabled state", func(t *testing.T) {
		p.ToggleSelected()
		if !p.disabled[name] {
			t.Errorf("expected %q to be disabled after toggle", name)
		}
	})

	t.Run("toggle again re-enables", func(t *testing.T) {
		p.ToggleSelected()
		if p.disabled[name] {
			t.Errorf("expected %q to be enabled after second toggle", name)
		}
	})
}

func TestToolPicker_DisabledMap(t *testing.T) {
	p := NewToolPicker(testToolNames(), map[string]bool{"bash": true, "grep": true})
	dm := p.DisabledMap()
	if len(dm) != 2 {
		t.Errorf("DisabledMap() len = %d, want 2", len(dm))
	}
	if !dm["bash"] || !dm["grep"] {
		t.Error("expected bash and grep to be disabled")
	}

	// Returned map should be a copy.
	dm["file_read"] = true
	if p.disabled["file_read"] {
		t.Error("expected DisabledMap to return a copy")
	}
}

func TestToolPicker_Dirty(t *testing.T) {
	p := NewToolPicker(testToolNames(), map[string]bool{"bash": true})

	t.Run("not dirty initially", func(t *testing.T) {
		if p.Dirty() {
			t.Error("expected not dirty initially")
		}
	})

	t.Run("dirty after toggle", func(t *testing.T) {
		p.ToggleSelected() // toggle bash (first sorted item)
		if !p.Dirty() {
			t.Error("expected dirty after toggle")
		}
	})

	t.Run("not dirty after mark applied", func(t *testing.T) {
		p.MarkApplied()
		if p.Dirty() {
			t.Error("expected not dirty after MarkApplied")
		}
	})
}

func TestToolPicker_ResetToBaseline(t *testing.T) {
	p := NewToolPicker(testToolNames(), map[string]bool{"bash": true})
	p.ToggleSelected() // un-disable bash
	if !p.Dirty() {
		t.Fatal("expected dirty before reset")
	}
	p.ResetToBaseline()
	if p.Dirty() {
		t.Error("expected not dirty after ResetToBaseline")
	}
	if !p.disabled["bash"] {
		t.Error("expected bash to be disabled again after reset")
	}
}

func TestToolPicker_Filter(t *testing.T) {
	p := NewToolPicker(testToolNames(), nil)

	t.Run("filter narrows results", func(t *testing.T) {
		p.AppendFilter('f')
		p.AppendFilter('i')
		p.AppendFilter('l')
		p.AppendFilter('e')
		// Should match file_read and file_write.
		if len(p.filtered) != 2 {
			t.Errorf("filtered = %d, want 2", len(p.filtered))
		}
		for _, name := range p.filtered {
			if !strings.HasPrefix(name, "file_") {
				t.Errorf("unexpected filtered name: %q", name)
			}
		}
	})

	t.Run("filter resets selectedIdx", func(t *testing.T) {
		if p.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0 after filter", p.selectedIdx)
		}
	})

	t.Run("backspace widens filter", func(t *testing.T) {
		p.BackspaceFilter()
		p.BackspaceFilter()
		p.BackspaceFilter()
		p.BackspaceFilter()
		if len(p.filtered) != len(p.names) {
			t.Errorf("filtered = %d, want %d after clearing filter", len(p.filtered), len(p.names))
		}
	})

	t.Run("backspace on empty is no-op", func(t *testing.T) {
		p.BackspaceFilter()
		if len(p.filtered) != len(p.names) {
			t.Error("backspace on empty filter should be no-op")
		}
	})

	t.Run("no matches yields empty", func(t *testing.T) {
		for _, r := range "zzzzz" {
			p.AppendFilter(r)
		}
		if len(p.filtered) != 0 {
			t.Errorf("filtered = %d, want 0 for non-matching filter", len(p.filtered))
		}
		if got := p.SelectedName(); got != "" {
			t.Errorf("SelectedName() = %q, want empty when no matches", got)
		}
	})
}

func TestToolPicker_View(t *testing.T) {
	p := NewToolPicker(testToolNames(), map[string]bool{"bash": true})
	view := p.View(80)

	t.Run("contains header", func(t *testing.T) {
		if !strings.Contains(view, "Tool Picker") {
			t.Error("expected view to contain 'Tool Picker'")
		}
	})

	t.Run("contains help text", func(t *testing.T) {
		if !strings.Contains(view, "Enter/Space=toggle") {
			t.Error("expected view to contain help text")
		}
	})

	t.Run("shows tool names", func(t *testing.T) {
		for _, name := range testToolNames() {
			if !strings.Contains(view, name) && !strings.Contains(view, strings.ReplaceAll(name, "_", " ")) {
				t.Errorf("expected view to contain tool %q or its display name", name)
			}
		}
	})

	t.Run("minimum width enforced", func(t *testing.T) {
		narrowView := p.View(10)
		if narrowView == "" {
			t.Error("expected non-empty view even with narrow width")
		}
	})

	t.Run("long MCP tool names are truncated with ellipsis", func(t *testing.T) {
		longNames := []string{"mcp__chrome-devtools__take_screenshot", "bash"}
		lp := NewToolPicker(longNames, nil)
		view := lp.View(60)
		if !strings.Contains(view, "…") {
			t.Error("expected long tool name to be truncated with ellipsis")
		}
	})
}
