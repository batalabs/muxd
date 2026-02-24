package tools

import (
	"fmt"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// task — sub-agent spawner
// ---------------------------------------------------------------------------

func taskTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "task",
			Description: "Spawn a sub-agent to handle a complex subtask. The sub-agent gets a fresh conversation with the same model and provider. It has all tools except task (no recursion). The sub-agent runs to completion and returns its output. Use this for independent subtasks that don't require the main conversation context.",
			Properties: map[string]provider.ToolProp{
				"description": {Type: "string", Description: "Short description of the subtask (3-5 words)"},
				"prompt":      {Type: "string", Description: "Detailed prompt for the sub-agent describing what to do"},
			},
			Required: []string{"description", "prompt"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.SpawnAgent == nil {
				return "", fmt.Errorf("sub-agent spawning not available")
			}

			description, _ := input["description"].(string)
			if description == "" {
				return "", fmt.Errorf("description is required")
			}

			prompt, _ := input["prompt"].(string)
			if prompt == "" {
				return "", fmt.Errorf("prompt is required")
			}

			result, err := ctx.SpawnAgent(description, prompt)
			if err != nil {
				return "", fmt.Errorf("sub-agent failed: %w", err)
			}

			// Output is already capped at 50KB by SpawnSubAgent.
			return result, nil
		},
	}
}

// IsSubAgentTool returns true for tool names that should not be available
// to sub-agents (to prevent recursion).
func IsSubAgentTool(name string) bool {
	return name == "task" || name == "schedule_task"
}

// AllToolsForSubAgent returns tools available to sub-agents (all except task).
func AllToolsForSubAgent() []ToolDef {
	all := AllTools()
	var filtered []ToolDef
	for _, t := range all {
		if !IsSubAgentTool(t.Spec.Name) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// AllToolSpecsForSubAgent returns tool specs available to sub-agents.
func AllToolSpecsForSubAgent() []provider.ToolSpec {
	tools := AllToolsForSubAgent()
	specs := make([]provider.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = t.Spec
	}
	return specs
}
