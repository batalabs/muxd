package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupTestServer creates an in-memory MCP server with the given tools and
// returns a connected Manager. The cleanup function closes everything.
func setupTestServer(t *testing.T, serverName string, mcpTools []*mcpsdk.Tool, handlers map[string]mcpsdk.ToolHandler) (*Manager, func()) {
	t.Helper()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "test-server",
		Version: "1.0",
	}, nil)

	for _, tool := range mcpTools {
		handler := handlers[tool.Name]
		if handler == nil {
			handler = func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}},
				}, nil
			}
		}
		server.AddTool(tool, handler)
	}

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ctx := context.Background()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	// Override newTransport to return the in-memory client transport.
	origTransport := newTransport
	newTransport = func(sc ServerConfig) (mcpsdk.Transport, context.CancelFunc) {
		return clientTransport, func() {}
	}

	mgr := NewManager()

	cfg := MCPConfig{
		MCPServers: map[string]ServerConfig{
			serverName: {Type: "stdio", Command: "unused"},
		},
	}
	if err := mgr.StartAll(ctx, cfg); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	return mgr, func() {
		mgr.StopAll()
		serverSession.Close()
		newTransport = origTransport
	}
}

func TestManager_ToolDiscovery(t *testing.T) {
	tools := []*mcpsdk.Tool{
		{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []any{"path", "content"},
			},
		},
	}

	mgr, cleanup := setupTestServer(t, "fs", tools, nil)
	defer cleanup()

	// Check tool names
	names := mgr.ToolNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 tool names, got %d: %v", len(names), names)
	}
	if names[0] != "mcp__fs__read_file" {
		t.Errorf("names[0] = %q, want mcp__fs__read_file", names[0])
	}
	if names[1] != "mcp__fs__write_file" {
		t.Errorf("names[1] = %q, want mcp__fs__write_file", names[1])
	}

	// Check tool specs
	specs := mgr.ToolSpecs()
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	// Check server statuses
	statuses := mgr.ServerStatuses()
	if statuses["fs"] != "connected" {
		t.Errorf("fs status = %q, want connected", statuses["fs"])
	}
}

func TestManager_CallTool(t *testing.T) {
	tools := []*mcpsdk.Tool{
		{
			Name:        "echo",
			Description: "Echo input",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
			},
		},
	}

	handlers := map[string]mcpsdk.ToolHandler{
		"echo": func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "no args"}},
					IsError: true,
				}, nil
			}
			msg, _ := args["message"].(string)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo: " + msg}},
			}, nil
		},
	}

	mgr, cleanup := setupTestServer(t, "echo-svc", tools, handlers)
	defer cleanup()

	result, isErr := mgr.CallTool(context.Background(), "echo-svc", "echo", map[string]any{
		"message": "hello",
	})
	if isErr {
		t.Errorf("unexpected error: %s", result)
	}
	if result != "echo: hello" {
		t.Errorf("result = %q, want %q", result, "echo: hello")
	}
}

func TestManager_CallTool_ServerNotFound(t *testing.T) {
	mgr := NewManager()
	result, isErr := mgr.CallTool(context.Background(), "nonexistent", "tool", nil)
	if !isErr {
		t.Error("expected isError=true for missing server")
	}
	if result == "" {
		t.Error("expected non-empty error message")
	}
}

func TestManager_CallTool_ErrorResult(t *testing.T) {
	tools := []*mcpsdk.Tool{
		{
			Name:        "fail",
			Description: "Always fails",
			InputSchema: map[string]any{"type": "object"},
		},
	}

	handlers := map[string]mcpsdk.ToolHandler{
		"fail": func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "something went wrong"}},
				IsError: true,
			}, nil
		},
	}

	mgr, cleanup := setupTestServer(t, "svc", tools, handlers)
	defer cleanup()

	result, isErr := mgr.CallTool(context.Background(), "svc", "fail", nil)
	if !isErr {
		t.Error("expected isError=true")
	}
	if result != "something went wrong" {
		t.Errorf("result = %q, want %q", result, "something went wrong")
	}
}

func TestManager_StopAll(t *testing.T) {
	tools := []*mcpsdk.Tool{
		{
			Name:        "ping",
			Description: "Ping",
			InputSchema: map[string]any{"type": "object"},
		},
	}

	mgr, cleanup := setupTestServer(t, "svc", tools, nil)
	defer cleanup()

	// Verify connected
	statuses := mgr.ServerStatuses()
	if statuses["svc"] != "connected" {
		t.Fatalf("expected connected, got %q", statuses["svc"])
	}

	mgr.StopAll()

	// After StopAll, server should be disconnected
	statuses = mgr.ServerStatuses()
	if statuses["svc"] != "disconnected" {
		t.Errorf("after StopAll: status = %q, want disconnected", statuses["svc"])
	}

	// Tool names should be empty (only connected servers contribute)
	names := mgr.ToolNames()
	if len(names) != 0 {
		t.Errorf("after StopAll: expected 0 tool names, got %d", len(names))
	}
}

func TestManager_ToolDefs(t *testing.T) {
	mcpTools := []*mcpsdk.Tool{
		{
			Name:        "greet",
			Description: "Greet someone",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
	}

	handlers := map[string]mcpsdk.ToolHandler{
		"greet": func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, fmt.Errorf("unmarshal args: %w", err)
			}
			name, _ := args["name"].(string)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "Hello, " + name + "!"}},
			}, nil
		},
	}

	mgr, cleanup := setupTestServer(t, "greeter", mcpTools, handlers)
	defer cleanup()

	defs := mgr.ToolDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def, got %d", len(defs))
	}

	def := defs[0]
	if def.Spec.Name != "mcp__greeter__greet" {
		t.Errorf("spec name = %q", def.Spec.Name)
	}

	result, err := def.Execute(map[string]any{"name": "World"}, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("result = %q, want %q", result, "Hello, World!")
	}
}

func TestManager_ConnectTimeout(t *testing.T) {
	// Verify the connectTimeout variable is accessible and reasonable.
	if connectTimeout < time.Second {
		t.Errorf("connectTimeout = %v, expected >= 1s", connectTimeout)
	}
}

func TestServerStatus_String(t *testing.T) {
	tests := []struct {
		status serverStatus
		expect string
	}{
		{statusDisconnected, "disconnected"},
		{statusConnecting, "connecting"},
		{statusConnected, "connected"},
		{statusError, "error"},
		{serverStatus(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expect {
				t.Errorf("String() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestManager_CallTool_Unavailable(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.servers["broken"] = &serverConn{
		name:   "broken",
		status: statusError,
		lastErr: fmt.Errorf("connection refused"),
	}
	mgr.mu.Unlock()

	result, isErr := mgr.CallTool(context.Background(), "broken", "tool", nil)
	if !isErr {
		t.Error("expected isError=true for unavailable server")
	}
	if result == "" {
		t.Error("expected non-empty error message")
	}
	// Should include the lastErr message
	if !strings.Contains(result, "connection refused") {
		t.Errorf("result %q should contain lastErr", result)
	}
}

func TestManager_CallTool_DisconnectedNoErr(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.servers["disc"] = &serverConn{
		name:   "disc",
		status: statusDisconnected,
	}
	mgr.mu.Unlock()

	result, isErr := mgr.CallTool(context.Background(), "disc", "tool", nil)
	if !isErr {
		t.Error("expected isError=true")
	}
	if !strings.Contains(result, "unavailable") {
		t.Errorf("result %q should mention unavailable", result)
	}
}

func TestManager_ServerStatuses_withError(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.servers["ok-svc"] = &serverConn{
		name:   "ok-svc",
		status: statusConnected,
	}
	mgr.servers["bad-svc"] = &serverConn{
		name:    "bad-svc",
		status:  statusError,
		lastErr: fmt.Errorf("timeout"),
	}
	mgr.mu.Unlock()

	statuses := mgr.ServerStatuses()
	if statuses["ok-svc"] != "connected" {
		t.Errorf("ok-svc = %q, want connected", statuses["ok-svc"])
	}
	if !strings.Contains(statuses["bad-svc"], "error") {
		t.Errorf("bad-svc = %q, want error prefix", statuses["bad-svc"])
	}
	if !strings.Contains(statuses["bad-svc"], "timeout") {
		t.Errorf("bad-svc = %q, should contain lastErr", statuses["bad-svc"])
	}
}

func TestManager_ToolSpecs_skipsDisconnected(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.servers["disc"] = &serverConn{
		name:   "disc",
		status: statusDisconnected,
		tools: []*mcpsdk.Tool{
			{Name: "hidden", Description: "should not appear"},
		},
	}
	mgr.mu.Unlock()

	specs := mgr.ToolSpecs()
	if len(specs) != 0 {
		t.Errorf("expected 0 specs from disconnected server, got %d", len(specs))
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name    string
		content []mcpsdk.Content
		expect  string
	}{
		{"nil", nil, ""},
		{"empty", []mcpsdk.Content{}, ""},
		{"single text", []mcpsdk.Content{&mcpsdk.TextContent{Text: "hello"}}, "hello"},
		{"multiple text", []mcpsdk.Content{
			&mcpsdk.TextContent{Text: "line1"},
			&mcpsdk.TextContent{Text: "line2"},
		}, "line1\nline2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextContent(tt.content); got != tt.expect {
				t.Errorf("extractTextContent() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestManager_ToolDefs_errorResult(t *testing.T) {
	mcpTools := []*mcpsdk.Tool{
		{
			Name:        "fail",
			Description: "Always fails",
			InputSchema: map[string]any{"type": "object"},
		},
	}

	handlers := map[string]mcpsdk.ToolHandler{
		"fail": func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "boom"}},
				IsError: true,
			}, nil
		},
	}

	mgr, cleanup := setupTestServer(t, "svc", mcpTools, handlers)
	defer cleanup()

	defs := mgr.ToolDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}

	result, err := defs[0].Execute(nil, nil)
	if err == nil {
		t.Fatal("expected error from Execute")
	}
	if result != "boom" {
		t.Errorf("result = %q, want %q", result, "boom")
	}
}
