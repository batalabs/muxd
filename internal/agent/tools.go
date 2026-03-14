package agent

import (
	"context"
	"fmt"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/tools"
)

// ExecuteToolCall runs a tool_use block and returns the result and error flag.
func ExecuteToolCall(call domain.ContentBlock, ctx *tools.ToolContext) (string, bool) {
	if ctx != nil && ctx.Disabled != nil && ctx.Disabled[call.ToolName] {
		return fmt.Sprintf("Tool %s is disabled by user config.", call.ToolName), true
	}

	// Route MCP tools to the MCP manager.
	if mcp.IsMCPTool(call.ToolName) {
		if ctx == nil || ctx.MCP == nil {
			return fmt.Sprintf("MCP tool %s called but no MCP manager configured", call.ToolName), true
		}
		server, toolName, ok := mcp.ParseNamespacedName(call.ToolName)
		if !ok {
			return fmt.Sprintf("Invalid MCP tool name: %s", call.ToolName), true
		}
		return ctx.MCP.CallTool(context.Background(), server, toolName, call.ToolInput)
	}

	planMode := false
	if ctx != nil && ctx.PlanMode != nil {
		planMode = *ctx.PlanMode
	}

	// Block built-in write tools in plan mode before looking them up.
	if planMode && isWriteTool(call.ToolName) {
		return fmt.Sprintf("Tool %s is disabled in plan mode. Use plan_exit to re-enable write tools.", call.ToolName), true
	}

	// Look up the tool, checking built-ins first then custom tools.
	var customRegistry *tools.CustomToolRegistry
	if ctx != nil {
		customRegistry = ctx.CustomTools
	}
	tool := tools.FindToolWithCustom(call.ToolName, customRegistry)
	if tool == nil {
		return fmt.Sprintf("Unknown tool: %s", call.ToolName), true
	}

	// Custom tools execute shell commands; block them when bash is disabled.
	if _, isBuiltin := tools.FindTool(call.ToolName); !isBuiltin {
		if ctx != nil && ctx.Disabled != nil && ctx.Disabled["bash"] {
			return fmt.Sprintf("Tool %s is disabled because bash execution is disabled.", call.ToolName), true
		}
	}

	result, err := tool.Execute(call.ToolInput, ctx)
	if err != nil {
		return err.Error(), true
	}
	return result, false
}

// isWriteTool checks if a tool name is a write tool (for plan mode error messages).
func isWriteTool(name string) bool {
	switch name {
	case "file_write", "file_edit", "bash", "patch_apply":
		return true
	}
	return false
}
