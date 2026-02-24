package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/tools"
)

// serverStatus describes the connection state of an MCP server.
type serverStatus int

const (
	statusDisconnected serverStatus = iota
	statusConnecting
	statusConnected
	statusError
)

func (s serverStatus) String() string {
	switch s {
	case statusDisconnected:
		return "disconnected"
	case statusConnecting:
		return "connecting"
	case statusConnected:
		return "connected"
	case statusError:
		return "error"
	default:
		return "unknown"
	}
}

// serverConn holds the state for a single MCP server connection.
type serverConn struct {
	name    string
	config  ServerConfig
	session *mcpsdk.ClientSession
	tools   []*mcpsdk.Tool
	cancel  context.CancelFunc
	status  serverStatus
	lastErr error
}

// Manager manages MCP server connections and tool discovery.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
}

// NewManager creates a new MCP server manager.
func NewManager() *Manager {
	return &Manager{
		servers: make(map[string]*serverConn),
	}
}

// connectTimeout is the timeout for connecting to a single MCP server.
var connectTimeout = 30 * time.Second

// StartAll connects to all configured MCP servers. Errors for individual
// servers are logged to stderr but do not prevent other servers from starting.
func (m *Manager) StartAll(ctx context.Context, cfg MCPConfig) error {
	for name, sc := range cfg.MCPServers {
		conn := &serverConn{
			name:   name,
			config: sc,
			status: statusConnecting,
		}
		m.mu.Lock()
		m.servers[name] = conn
		m.mu.Unlock()

		if err := m.connectServer(ctx, conn); err != nil {
			m.mu.Lock()
			conn.status = statusError
			conn.lastErr = err
			m.mu.Unlock()
			fmt.Fprintf(os.Stderr, "mcp: server %q failed to connect: %v\n", name, err)
			continue
		}

		m.mu.Lock()
		conn.status = statusConnected
		m.mu.Unlock()
	}
	return nil
}

// newTransport creates the appropriate MCP transport. Extracted for testability.
var newTransport = defaultNewTransport

func defaultNewTransport(sc ServerConfig) (mcpsdk.Transport, context.CancelFunc) {
	switch sc.Type {
	case "http":
		return &mcpsdk.StreamableClientTransport{Endpoint: sc.URL}, func() {}
	default: // stdio
		cmd := exec.Command(sc.Command, sc.Args...)
		if len(sc.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range sc.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		// Discard child stderr to avoid polluting the TUI.
		return &mcpsdk.CommandTransport{Command: cmd}, func() {
			if cmd.Process != nil {
				// Ignore "invalid argument" / "process already finished" errors
				// that occur when the child has already exited.
				_ = cmd.Process.Kill()
			}
		}
	}
}

func (m *Manager) connectServer(ctx context.Context, conn *serverConn) error {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "muxd",
		Version: "1.0",
	}, nil)

	transport, killFunc := newTransport(conn.config)

	connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	session, err := client.Connect(connCtx, transport, nil)
	if err != nil {
		killFunc()
		return fmt.Errorf("connecting: %w", err)
	}

	conn.cancel = killFunc

	conn.session = session

	// Discover tools
	listCtx, listCancel := context.WithTimeout(ctx, connectTimeout)
	defer listCancel()

	result, err := session.ListTools(listCtx, nil)
	if err != nil {
		conn.cancel()
		return fmt.Errorf("listing tools: %w", err)
	}
	conn.tools = result.Tools
	return nil
}

// StopAll closes all MCP server connections.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, conn := range m.servers {
		if conn.session != nil {
			if err := conn.session.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "mcp: close session: %v\n", err)
			}
		}
		if conn.cancel != nil {
			conn.cancel()
		}
		conn.status = statusDisconnected
	}
}

// ToolSpecs returns all MCP tools as namespaced provider.ToolSpecs.
func (m *Manager) ToolSpecs() []provider.ToolSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var specs []provider.ToolSpec
	for _, conn := range m.servers {
		if conn.status != statusConnected {
			continue
		}
		for _, tool := range conn.tools {
			specs = append(specs, ToToolSpec(conn.name, tool))
		}
	}
	return specs
}

// ToolDefs returns all MCP tools as ToolDefs with execute functions.
func (m *Manager) ToolDefs() []tools.ToolDef {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var defs []tools.ToolDef
	for _, conn := range m.servers {
		if conn.status != statusConnected {
			continue
		}
		for _, tool := range conn.tools {
			spec := ToToolSpec(conn.name, tool)
			serverName := conn.name
			toolName := tool.Name
			defs = append(defs, tools.ToolDef{
				Spec: spec,
				Execute: func(input map[string]any, ctx *tools.ToolContext) (string, error) {
					result, isErr := m.CallTool(context.Background(), serverName, toolName, input)
					if isErr {
						return result, fmt.Errorf("%s", result)
					}
					return result, nil
				},
			})
		}
	}
	return defs
}

// CallTool invokes an MCP tool on the named server.
// Returns (result text, isError).
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, bool) {
	m.mu.RLock()
	conn, ok := m.servers[serverName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Sprintf("MCP server %q not found", serverName), true
	}
	if conn.status != statusConnected || conn.session == nil {
		errMsg := fmt.Sprintf("MCP server %q is unavailable", serverName)
		if conn.lastErr != nil {
			errMsg += ": " + conn.lastErr.Error()
		}
		return errMsg, true
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := conn.session.CallTool(callCtx, &mcpsdk.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return "MCP tool call timed out after 30s", true
		}
		return fmt.Sprintf("MCP tool call failed: %v", err), true
	}

	if result == nil {
		return "MCP server returned empty response", true
	}

	text := extractTextContent(result.Content)
	if text == "" {
		return "MCP server returned empty response", true
	}

	return text, result.IsError
}

// extractTextContent concatenates text from MCP Content items.
func extractTextContent(content []mcpsdk.Content) string {
	var parts []string
	for _, c := range content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ToolNames returns a sorted list of all MCP tool names (namespaced).
func (m *Manager) ToolNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for _, conn := range m.servers {
		if conn.status != statusConnected {
			continue
		}
		for _, tool := range conn.tools {
			names = append(names, NamespacedName(conn.name, tool.Name))
		}
	}
	sort.Strings(names)
	return names
}

// ServerStatuses returns the connection status for each server.
func (m *Manager) ServerStatuses() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make(map[string]string, len(m.servers))
	for name, conn := range m.servers {
		s := conn.status.String()
		if conn.lastErr != nil && conn.status == statusError {
			s += ": " + conn.lastErr.Error()
		}
		statuses[name] = s
	}
	return statuses
}
