package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// writeTools is the set of tool names disabled during plan mode.
var writeTools = map[string]bool{
	"file_write":  true,
	"file_edit":   true,
	"bash":        true,
	"patch_apply": true,
}

// ---------------------------------------------------------------------------
// plan_enter
// ---------------------------------------------------------------------------

func planEnterTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "plan_enter",
			Description: "Enter plan mode. In plan mode, write tools (file_write, file_edit, bash, patch_apply) are disabled. Only read/search tools remain available. Use this when you want to explore and plan before making changes.",
			Properties:  map[string]provider.ToolProp{},
			Required:    []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.PlanMode == nil {
				return "", fmt.Errorf("plan mode not available")
			}
			if *ctx.PlanMode {
				return "Already in plan mode.", nil
			}
			*ctx.PlanMode = true
			return "Entered plan mode. Write tools are now disabled. Use plan_exit to resume editing.", nil
		},
	}
}

// ---------------------------------------------------------------------------
// plan_exit
// ---------------------------------------------------------------------------

func planExitTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "plan_exit",
			Description: "Exit plan mode and re-enable write tools (file_write, file_edit, bash, patch_apply). Use this when you're ready to implement your plan.",
			Properties:  map[string]provider.ToolProp{},
			Required:    []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.PlanMode == nil {
				return "", fmt.Errorf("plan mode not available")
			}
			if !*ctx.PlanMode {
				return "Not in plan mode.", nil
			}
			*ctx.PlanMode = false
			return "Exited plan mode. All tools are now available.", nil
		},
	}
}

// ---------------------------------------------------------------------------
// Mode-aware tool filtering
// ---------------------------------------------------------------------------

// AllToolsForMode returns tools filtered by plan mode. In plan mode,
// write tools are excluded.
func AllToolsForMode(planMode bool) []ToolDef {
	all := AllTools()
	if !planMode {
		return all
	}
	var filtered []ToolDef
	for _, t := range all {
		if !writeTools[t.Spec.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// AllToolSpecsForMode returns tool specs filtered by plan mode.
func AllToolSpecsForMode(planMode bool) []provider.ToolSpec {
	tools := AllToolsForMode(planMode)
	specs := make([]provider.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = t.Spec
	}
	return specs
}

// AllToolSpecsForModeWithDisabled returns tool specs filtered by plan mode
// and a disabled-tools set.
func AllToolSpecsForModeWithDisabled(planMode bool, disabled map[string]bool) []provider.ToolSpec {
	tools := AllToolsForMode(planMode)
	var specs []provider.ToolSpec
	for _, t := range tools {
		if disabled != nil && disabled[t.Spec.Name] {
			continue
		}
		specs = append(specs, t.Spec)
	}
	return specs
}

// FindToolForMode looks up a tool by name, respecting plan mode.
func FindToolForMode(name string, planMode bool) (ToolDef, bool) {
	if planMode && writeTools[name] {
		return ToolDef{}, false
	}
	return FindTool(name)
}

// WriteToolNames returns a comma-separated list of write tool names
// (for use in error messages).
func WriteToolNames() string {
	var names []string
	for name := range writeTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
