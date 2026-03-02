package tools

import (
	"strings"
	"testing"
)

func TestPlanEnterTool(t *testing.T) {
	tool := planEnterTool()

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{}, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("enters plan mode", func(t *testing.T) {
		planMode := false
		ctx := &ToolContext{PlanMode: &planMode}
		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !planMode {
			t.Error("expected planMode=true")
		}
		if !strings.Contains(result, "Entered plan mode") {
			t.Errorf("expected enter message, got: %s", result)
		}
	})

	t.Run("already in plan mode", func(t *testing.T) {
		planMode := true
		ctx := &ToolContext{PlanMode: &planMode}
		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Already") {
			t.Errorf("expected already message, got: %s", result)
		}
	})
}

func TestPlanExitTool(t *testing.T) {
	tool := planExitTool()

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{}, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("exits plan mode", func(t *testing.T) {
		planMode := true
		ctx := &ToolContext{PlanMode: &planMode}
		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if planMode {
			t.Error("expected planMode=false")
		}
		if !strings.Contains(result, "Exited plan mode") {
			t.Errorf("expected exit message, got: %s", result)
		}
	})

	t.Run("not in plan mode", func(t *testing.T) {
		planMode := false
		ctx := &ToolContext{PlanMode: &planMode}
		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Not in plan mode") {
			t.Errorf("expected not in plan mode message, got: %s", result)
		}
	})
}

func TestAllToolsForMode(t *testing.T) {
	t.Run("normal mode returns all tools", func(t *testing.T) {
		all := AllToolsForMode(false)
		allSpecs := AllTools()
		if len(all) != len(allSpecs) {
			t.Errorf("expected %d tools, got %d", len(allSpecs), len(all))
		}
	})

	t.Run("plan mode excludes write tools", func(t *testing.T) {
		filtered := AllToolsForMode(true)
		for _, tool := range filtered {
			if writeTools[tool.Spec.Name] {
				t.Errorf("write tool %s should be excluded in plan mode", tool.Spec.Name)
			}
		}
		// Verify some read tools are present.
		names := make(map[string]bool)
		for _, tool := range filtered {
			names[tool.Spec.Name] = true
		}
		for _, expected := range []string{"file_read", "grep", "list_files", "plan_enter", "plan_exit", "todo_read"} {
			if !names[expected] {
				t.Errorf("expected %s in plan mode tools", expected)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// AllToolSpecsForModeWithDisabled
// ---------------------------------------------------------------------------

func TestAllToolSpecsForModeWithDisabled(t *testing.T) {
	t.Run("normal mode with disabled set", func(t *testing.T) {
		disabled := map[string]bool{"bash": true, "sms_send": true}
		specs := AllToolSpecsForModeWithDisabled(false, disabled)
		for _, s := range specs {
			if s.Name == "bash" || s.Name == "sms_send" {
				t.Errorf("disabled tool %q should be excluded", s.Name)
			}
		}
		// file_read should be present.
		found := false
		for _, s := range specs {
			if s.Name == "file_read" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected file_read in specs")
		}
	})

	t.Run("plan mode with disabled set", func(t *testing.T) {
		disabled := map[string]bool{"grep": true}
		specs := AllToolSpecsForModeWithDisabled(true, disabled)
		for _, s := range specs {
			if s.Name == "grep" {
				t.Error("disabled tool grep should be excluded")
			}
			if writeTools[s.Name] {
				t.Errorf("write tool %q should be excluded in plan mode", s.Name)
			}
		}
	})

	t.Run("nil disabled set", func(t *testing.T) {
		specs := AllToolSpecsForModeWithDisabled(false, nil)
		all := AllTools()
		if len(specs) != len(all) {
			t.Errorf("expected %d specs, got %d", len(all), len(specs))
		}
	})
}

// ---------------------------------------------------------------------------
// WriteToolNames
// ---------------------------------------------------------------------------

func TestWriteToolNames(t *testing.T) {
	result := WriteToolNames()
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Should contain all write tools.
	for name := range writeTools {
		if !strings.Contains(result, name) {
			t.Errorf("expected %q in WriteToolNames(), got: %q", name, result)
		}
	}
	// Should be comma-separated.
	if !strings.Contains(result, ", ") {
		t.Errorf("expected comma-separated list, got: %q", result)
	}
}

// ---------------------------------------------------------------------------
// FindToolForMode
// ---------------------------------------------------------------------------

func TestFindToolForMode(t *testing.T) {
	t.Run("normal mode finds all tools", func(t *testing.T) {
		_, ok := FindToolForMode("file_write", false)
		if !ok {
			t.Error("expected to find file_write in normal mode")
		}
	})

	t.Run("plan mode blocks write tools", func(t *testing.T) {
		_, ok := FindToolForMode("file_write", true)
		if ok {
			t.Error("expected file_write blocked in plan mode")
		}
		_, ok = FindToolForMode("bash", true)
		if ok {
			t.Error("expected bash blocked in plan mode")
		}
	})

	t.Run("plan mode allows read tools", func(t *testing.T) {
		_, ok := FindToolForMode("file_read", true)
		if !ok {
			t.Error("expected file_read allowed in plan mode")
		}
		_, ok = FindToolForMode("grep", true)
		if !ok {
			t.Error("expected grep allowed in plan mode")
		}
	})
}
