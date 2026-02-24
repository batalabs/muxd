package mcp

import "testing"

func TestNamespacedName(t *testing.T) {
	tests := []struct {
		name       string
		serverName string
		toolName   string
		want       string
	}{
		{
			name:       "simple names",
			serverName: "fs",
			toolName:   "read_file",
			want:       "mcp__fs__read_file",
		},
		{
			name:       "server name with uppercase",
			serverName: "MyServer",
			toolName:   "do_thing",
			want:       "mcp__myserver__do_thing",
		},
		{
			name:       "server name with special characters",
			serverName: "my.server_name",
			toolName:   "list",
			want:       "mcp__my-server-name__list",
		},
		{
			name:       "hyphenated server",
			serverName: "my-db",
			toolName:   "query",
			want:       "mcp__my-db__query",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NamespacedName(tt.serverName, tt.toolName)
			if got != tt.want {
				t.Errorf("NamespacedName(%q, %q) = %q, want %q", tt.serverName, tt.toolName, got, tt.want)
			}
		})
	}
}

func TestParseNamespacedName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantServer string
		wantTool   string
		wantOK     bool
	}{
		{
			name:       "valid mcp tool",
			input:      "mcp__fs__read_file",
			wantServer: "fs",
			wantTool:   "read_file",
			wantOK:     true,
		},
		{
			name:       "not an mcp tool",
			input:      "file_read",
			wantServer: "",
			wantTool:   "",
			wantOK:     false,
		},
		{
			name:       "prefix only",
			input:      "mcp__",
			wantServer: "",
			wantTool:   "",
			wantOK:     false,
		},
		{
			name:       "missing tool part",
			input:      "mcp__server",
			wantServer: "",
			wantTool:   "",
			wantOK:     false,
		},
		{
			name:       "empty server name",
			input:      "mcp____tool",
			wantServer: "",
			wantTool:   "",
			wantOK:     false,
		},
		{
			name:       "tool with double underscore in tool name",
			input:      "mcp__db__get__item",
			wantServer: "db",
			wantTool:   "get__item",
			wantOK:     true,
		},
		{
			name:       "empty tool name after server",
			input:      "mcp__server__",
			wantServer: "",
			wantTool:   "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, tool, ok := ParseNamespacedName(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ParseNamespacedName(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if server != tt.wantServer {
				t.Errorf("ParseNamespacedName(%q) server = %q, want %q", tt.input, server, tt.wantServer)
			}
			if tool != tt.wantTool {
				t.Errorf("ParseNamespacedName(%q) tool = %q, want %q", tt.input, tool, tt.wantTool)
			}
		})
	}
}

func TestIsMCPTool(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"mcp tool", "mcp__fs__read", true},
		{"built-in tool", "file_read", false},
		{"partial prefix", "mcp_tool", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMCPTool(tt.input)
			if got != tt.want {
				t.Errorf("IsMCPTool(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	name := NamespacedName("my-server", "do_thing")
	server, tool, ok := ParseNamespacedName(name)
	if !ok {
		t.Fatalf("ParseNamespacedName(%q) returned ok=false", name)
	}
	if server != "my-server" {
		t.Errorf("server = %q, want %q", server, "my-server")
	}
	if tool != "do_thing" {
		t.Errorf("tool = %q, want %q", tool, "do_thing")
	}
}
