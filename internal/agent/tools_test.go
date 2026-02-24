package agent

import (
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/tools"
)

func TestIsWriteTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"file_write", true},
		{"file_edit", true},
		{"bash", true},
		{"patch_apply", true},
		{"file_read", false},
		{"grep", false},
		{"list_files", false},
		{"ask_user", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWriteTool(tt.name); got != tt.want {
				t.Errorf("isWriteTool(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestExecuteToolCall_disabledTool(t *testing.T) {
	call := domain.ContentBlock{
		Type:      "tool_use",
		ToolUseID: "tu_1",
		ToolName:  "bash",
		ToolInput: map[string]any{"command": "echo hi"},
	}
	ctx := &tools.ToolContext{
		Disabled: map[string]bool{"bash": true},
	}
	result, isError := ExecuteToolCall(call, ctx)
	if !isError {
		t.Fatal("expected isError=true for disabled tool")
	}
	if result != "Tool bash is disabled by user config." {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestExecuteToolCall_planModeBlocksWriteTools(t *testing.T) {
	planMode := true
	call := domain.ContentBlock{
		Type:      "tool_use",
		ToolUseID: "tu_1",
		ToolName:  "file_write",
		ToolInput: map[string]any{"path": "/tmp/x", "content": "y"},
	}
	ctx := &tools.ToolContext{
		PlanMode: &planMode,
	}
	result, isError := ExecuteToolCall(call, ctx)
	if !isError {
		t.Fatal("expected isError=true for write tool in plan mode")
	}
	if result == "" {
		t.Fatal("expected non-empty error result")
	}
}

func TestExecuteToolCall_invalidMCPTool(t *testing.T) {
	call := domain.ContentBlock{
		Type:      "tool_use",
		ToolUseID: "tu_mcp",
		ToolName:  "mcp__server__tool",
		ToolInput: map[string]any{},
	}
	// No MCP manager configured
	result, isError := ExecuteToolCall(call, nil)
	if !isError {
		t.Fatal("expected isError=true for MCP tool with no manager")
	}
	if result == "" {
		t.Fatal("expected non-empty error result")
	}
}
