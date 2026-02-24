package tools

import (
	"strings"
	"testing"
)

func TestTaskTool(t *testing.T) {
	tool := taskTool()

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"description": "test",
			"prompt":      "do something",
		}, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("nil SpawnAgent returns error", func(t *testing.T) {
		ctx := &ToolContext{}
		_, err := tool.Execute(map[string]any{
			"description": "test",
			"prompt":      "do something",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for nil SpawnAgent")
		}
	})

	t.Run("missing description returns error", func(t *testing.T) {
		ctx := &ToolContext{
			SpawnAgent: func(desc, prompt string) (string, error) { return "", nil },
		}
		_, err := tool.Execute(map[string]any{
			"prompt": "do something",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for missing description")
		}
	})

	t.Run("missing prompt returns error", func(t *testing.T) {
		ctx := &ToolContext{
			SpawnAgent: func(desc, prompt string) (string, error) { return "", nil },
		}
		_, err := tool.Execute(map[string]any{
			"description": "test",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for missing prompt")
		}
	})

	t.Run("successful spawn returns result", func(t *testing.T) {
		ctx := &ToolContext{
			SpawnAgent: func(desc, prompt string) (string, error) {
				return "Sub-agent result: " + desc, nil
			},
		}
		result, err := tool.Execute(map[string]any{
			"description": "search files",
			"prompt":      "find all Go files",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Sub-agent result") {
			t.Errorf("expected sub-agent result, got: %s", result)
		}
	})

	t.Run("spawn error is propagated", func(t *testing.T) {
		ctx := &ToolContext{
			SpawnAgent: func(desc, prompt string) (string, error) {
				return "", &testError{msg: "agent crashed"}
			},
		}
		_, err := tool.Execute(map[string]any{
			"description": "test",
			"prompt":      "do something",
		}, ctx)
		if err == nil {
			t.Fatal("expected error from spawn")
		}
		if !strings.Contains(err.Error(), "agent crashed") {
			t.Errorf("expected 'agent crashed' in error, got: %v", err)
		}
	})

	t.Run("large output passes through (truncation is in SpawnSubAgent)", func(t *testing.T) {
		largeOutput := strings.Repeat("x", 60*1024)
		ctx := &ToolContext{
			SpawnAgent: func(desc, prompt string) (string, error) {
				return largeOutput, nil
			},
		}
		result, err := tool.Execute(map[string]any{
			"description": "test",
			"prompt":      "do something",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != len(largeOutput) {
			t.Errorf("expected full output (%d bytes), got %d bytes", len(largeOutput), len(result))
		}
	})
}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// IsSubAgentTool
// ---------------------------------------------------------------------------

func TestIsSubAgentTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"task", true},
		{"file_read", false},
		{"bash", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSubAgentTool(tt.name)
			if got != tt.want {
				t.Errorf("IsSubAgentTool(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AllToolSpecsForSubAgent
// ---------------------------------------------------------------------------

func TestAllToolSpecsForSubAgent(t *testing.T) {
	specs := AllToolSpecsForSubAgent()
	for _, s := range specs {
		if s.Name == "task" {
			t.Error("sub-agent specs should not include task")
		}
	}
	// Verify count matches AllToolsForSubAgent.
	tools := AllToolsForSubAgent()
	if len(specs) != len(tools) {
		t.Errorf("specs count %d != tools count %d", len(specs), len(tools))
	}
}

// ---------------------------------------------------------------------------
// AllToolsForSubAgent
// ---------------------------------------------------------------------------

func TestAllToolsForSubAgent(t *testing.T) {
	subTools := AllToolsForSubAgent()
	for _, tool := range subTools {
		if tool.Spec.Name == "task" {
			t.Error("sub-agent should not have task tool")
		}
	}
	// Verify other tools are present.
	names := make(map[string]bool)
	for _, tool := range subTools {
		names[tool.Spec.Name] = true
	}
	for _, expected := range []string{"file_read", "file_write", "bash", "grep"} {
		if !names[expected] {
			t.Errorf("expected %s in sub-agent tools", expected)
		}
	}
}
