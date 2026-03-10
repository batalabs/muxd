package tools

import (
	"fmt"

	"github.com/batalabs/muxd/internal/provider"
)

func hubDispatchTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "hub_dispatch",
			Description: "Dispatch a task to a remote node. The node runs a full agent loop and returns the output. Use hub_discovery first to find available nodes and their capabilities.",
			Properties: map[string]provider.ToolProp{
				"node": {
					Type:        "string",
					Description: "Node name or ID to dispatch the task to",
				},
				"prompt": {
					Type:        "string",
					Description: "Task prompt for the remote agent to execute",
				},
			},
			Required: []string{"node", "prompt"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx.HubDispatch == nil {
				return "", fmt.Errorf("not connected to a hub — hub_dispatch requires a hub connection")
			}

			node, _ := input["node"].(string)
			if node == "" {
				return "", fmt.Errorf("node is required")
			}

			prompt, _ := input["prompt"].(string)
			if prompt == "" {
				return "", fmt.Errorf("prompt is required")
			}

			result, err := ctx.HubDispatch(node, prompt)
			if err != nil {
				return "", fmt.Errorf("hub dispatch: %w", err)
			}

			return result, nil
		},
	}
}
