package tools

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// HubNodeInfo is a tool-layer representation of a hub node's capabilities.
type HubNodeInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Version  string   `json:"version"`
	Platform string   `json:"platform"`
	Arch     string   `json:"arch"`
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Tools    []string `json:"tools"`
	MCPTools []string `json:"mcp_tools"`
}

func hubDiscoveryTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "hub_discovery",
			Description: "Discover nodes connected to the muxd hub. List all nodes with their platform, model, and available tools.",
			Properties: map[string]provider.ToolProp{
				"action": {
					Type:        "string",
					Description: "Action to perform",
					Enum:        []string{"list_nodes", "node_tools"},
				},
				"node": {
					Type:        "string",
					Description: "Node name or ID (required for node_tools action)",
				},
			},
			Required: []string{"action"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx.HubDiscovery == nil {
				return "", fmt.Errorf("not connected to a hub — hub_discovery requires a hub connection")
			}

			action, _ := input["action"].(string)

			nodes, err := ctx.HubDiscovery()
			if err != nil {
				return "", fmt.Errorf("hub discovery: %w", err)
			}

			switch action {
			case "list_nodes":
				return formatNodeList(nodes), nil

			case "node_tools":
				nodeName, _ := input["node"].(string)
				if nodeName == "" {
					return "", fmt.Errorf("node name or ID is required for node_tools action")
				}
				node := findNode(nodes, nodeName)
				if node == nil {
					return "", fmt.Errorf("node %q not found", nodeName)
				}
				return formatNodeTools(node), nil

			default:
				return "", fmt.Errorf("unknown action %q — use list_nodes or node_tools", action)
			}
		},
	}
}

func findNode(nodes []HubNodeInfo, nameOrID string) *HubNodeInfo {
	lower := strings.ToLower(nameOrID)
	for i := range nodes {
		if strings.ToLower(nodes[i].Name) == lower || nodes[i].ID == nameOrID {
			return &nodes[i]
		}
	}
	return nil
}

func formatNodeList(nodes []HubNodeInfo) string {
	if len(nodes) == 0 {
		return "No nodes connected to the hub."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d node(s) connected:\n\n", len(nodes))
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s\n", n.Name)
		fmt.Fprintf(&b, "    Status:   %s\n", n.Status)
		fmt.Fprintf(&b, "    Platform: %s/%s\n", n.Platform, n.Arch)
		if n.Provider != "" && n.Model != "" {
			fmt.Fprintf(&b, "    Model:    %s/%s\n", n.Provider, n.Model)
		}
		if n.Version != "" {
			fmt.Fprintf(&b, "    Version:  %s\n", n.Version)
		}
		fmt.Fprintf(&b, "    Tools:    %d built-in", len(n.Tools))
		if len(n.MCPTools) > 0 {
			fmt.Fprintf(&b, ", %d MCP", len(n.MCPTools))
		}
		fmt.Fprintf(&b, "\n\n")
	}
	return b.String()
}

func formatNodeTools(node *HubNodeInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Node: %s (%s/%s)\n\n", node.Name, node.Platform, node.Arch)

	if len(node.Tools) > 0 {
		fmt.Fprintf(&b, "Built-in tools (%d):\n", len(node.Tools))
		for _, t := range node.Tools {
			fmt.Fprintf(&b, "  - %s\n", t)
		}
	}

	if len(node.MCPTools) > 0 {
		fmt.Fprintf(&b, "\nMCP tools (%d):\n", len(node.MCPTools))
		for _, t := range node.MCPTools {
			fmt.Fprintf(&b, "  - %s\n", t)
		}
	}

	if len(node.Tools) == 0 && len(node.MCPTools) == 0 {
		fmt.Fprintf(&b, "No tool information available (node may be running an older version).\n")
	}

	return b.String()
}
