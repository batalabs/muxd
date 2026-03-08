package tools

import (
	"strings"
	"testing"
)

func TestHubDiscoveryTool_NoHub(t *testing.T) {
	tool := hubDiscoveryTool()
	ctx := &ToolContext{}

	_, err := tool.Execute(map[string]any{"action": "list_nodes"}, ctx)
	if err == nil {
		t.Fatal("expected error when not connected to hub")
	}
	if !strings.Contains(err.Error(), "not connected to a hub") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHubDiscoveryTool_ListNodes(t *testing.T) {
	tool := hubDiscoveryTool()
	nodes := []HubNodeInfo{
		{
			ID: "id1", Name: "desktop", Status: "online",
			Platform: "windows", Arch: "amd64",
			Provider: "anthropic", Model: "claude-sonnet",
			Version: "0.5.0",
			Tools:   []string{"bash", "file_read"},
		},
		{
			ID: "id2", Name: "server01", Status: "online",
			Platform: "linux", Arch: "arm64",
			Provider: "ollama", Model: "llama3",
			MCPTools: []string{"mcp__fs__read"},
		},
	}
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nodes, nil },
	}

	result, err := tool.Execute(map[string]any{"action": "list_nodes"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "2 node(s) connected") {
		t.Errorf("expected 2 nodes in output, got: %s", result)
	}
	if !strings.Contains(result, "desktop") {
		t.Error("expected 'desktop' in output")
	}
	if !strings.Contains(result, "server01") {
		t.Error("expected 'server01' in output")
	}
	if !strings.Contains(result, "anthropic/claude-sonnet") {
		t.Error("expected model info in output")
	}
}

func TestHubDiscoveryTool_ListNodes_Empty(t *testing.T) {
	tool := hubDiscoveryTool()
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nil, nil },
	}

	result, err := tool.Execute(map[string]any{"action": "list_nodes"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No nodes connected") {
		t.Errorf("expected empty message, got: %s", result)
	}
}

func TestHubDiscoveryTool_NodeTools(t *testing.T) {
	tool := hubDiscoveryTool()
	nodes := []HubNodeInfo{
		{
			ID: "id1", Name: "desktop", Status: "online",
			Platform: "windows", Arch: "amd64",
			Tools:    []string{"bash", "file_read", "grep"},
			MCPTools: []string{"mcp__fs__read", "mcp__fs__write"},
		},
	}
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nodes, nil },
	}

	result, err := tool.Execute(map[string]any{"action": "node_tools", "node": "desktop"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "bash") {
		t.Error("expected 'bash' in tools output")
	}
	if !strings.Contains(result, "mcp__fs__read") {
		t.Error("expected MCP tool in output")
	}
	if !strings.Contains(result, "Built-in tools (3)") {
		t.Error("expected built-in tool count")
	}
	if !strings.Contains(result, "MCP tools (2)") {
		t.Error("expected MCP tool count")
	}
}

func TestHubDiscoveryTool_NodeTools_NotFound(t *testing.T) {
	tool := hubDiscoveryTool()
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nil, nil },
	}

	_, err := tool.Execute(map[string]any{"action": "node_tools", "node": "nonexistent"}, ctx)
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHubDiscoveryTool_NodeTools_CaseInsensitive(t *testing.T) {
	tool := hubDiscoveryTool()
	nodes := []HubNodeInfo{
		{ID: "id1", Name: "MyDesktop", Platform: "windows", Arch: "amd64", Tools: []string{"bash"}},
	}
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nodes, nil },
	}

	result, err := tool.Execute(map[string]any{"action": "node_tools", "node": "mydesktop"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "MyDesktop") {
		t.Error("expected 'MyDesktop' in output")
	}
}

func TestHubDiscoveryTool_UnknownAction(t *testing.T) {
	tool := hubDiscoveryTool()
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nil, nil },
	}

	_, err := tool.Execute(map[string]any{"action": "invalid"}, ctx)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHubDiscoveryTool_NodeTools_MissingNode(t *testing.T) {
	tool := hubDiscoveryTool()
	ctx := &ToolContext{
		HubDiscovery: func() ([]HubNodeInfo, error) { return nil, nil },
	}

	_, err := tool.Execute(map[string]any{"action": "node_tools"}, ctx)
	if err == nil {
		t.Fatal("expected error for missing node param")
	}
	if !strings.Contains(err.Error(), "node name or ID is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
