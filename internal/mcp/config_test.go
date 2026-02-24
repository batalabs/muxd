package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMCPConfig_ProjectScope(t *testing.T) {
	dir := t.TempDir()
	data := `{"mcpServers":{"fs":{"type":"stdio","command":"npx","args":["-y","@mcp/server-fs","."]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	// Disable user config for this test.
	origDir := userConfigDir
	userConfigDir = func() string { return "" }
	defer func() { userConfigDir = origDir }()

	cfg, err := LoadMCPConfig(dir)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.MCPServers))
	}
	fs, ok := cfg.MCPServers["fs"]
	if !ok {
		t.Fatal("server 'fs' not found")
	}
	if fs.Command != "npx" {
		t.Errorf("command = %q, want %q", fs.Command, "npx")
	}
	if len(fs.Args) != 3 {
		t.Errorf("args len = %d, want 3", len(fs.Args))
	}
}

func TestLoadMCPConfig_UserScope(t *testing.T) {
	userDir := t.TempDir()
	data := `{"mcpServers":{"db":{"type":"http","url":"http://localhost:3000"}}}`
	if err := os.WriteFile(filepath.Join(userDir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir := userConfigDir
	userConfigDir = func() string { return userDir }
	defer func() { userConfigDir = origDir }()

	cfg, err := LoadMCPConfig(t.TempDir()) // empty project dir
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.MCPServers))
	}
	db, ok := cfg.MCPServers["db"]
	if !ok {
		t.Fatal("server 'db' not found")
	}
	if db.URL != "http://localhost:3000" {
		t.Errorf("url = %q, want %q", db.URL, "http://localhost:3000")
	}
}

func TestLoadMCPConfig_MergeProjectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	userData := `{"mcpServers":{"fs":{"type":"stdio","command":"user-cmd"},"db":{"type":"http","url":"http://user:3000"}}}`
	if err := os.WriteFile(filepath.Join(userDir, "mcp.json"), []byte(userData), 0o644); err != nil {
		t.Fatal(err)
	}

	projDir := t.TempDir()
	projData := `{"mcpServers":{"fs":{"type":"stdio","command":"proj-cmd"}}}`
	if err := os.WriteFile(filepath.Join(projDir, ".mcp.json"), []byte(projData), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir := userConfigDir
	userConfigDir = func() string { return userDir }
	defer func() { userConfigDir = origDir }()

	cfg, err := LoadMCPConfig(projDir)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cfg.MCPServers))
	}
	// Project should override user for "fs"
	if cfg.MCPServers["fs"].Command != "proj-cmd" {
		t.Errorf("fs command = %q, want %q", cfg.MCPServers["fs"].Command, "proj-cmd")
	}
	// User "db" should still be present
	if cfg.MCPServers["db"].URL != "http://user:3000" {
		t.Errorf("db url = %q, want %q", cfg.MCPServers["db"].URL, "http://user:3000")
	}
}

func TestLoadMCPConfig_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	data := `{"mcpServers":{"svc":{"type":"http","url":"${TEST_MCP_URL:-http://fallback:8080}"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir := userConfigDir
	userConfigDir = func() string { return "" }
	defer func() { userConfigDir = origDir }()

	origEnv := lookupEnvFunc
	defer func() { lookupEnvFunc = origEnv }()

	// Test with env var not set — should use default
	lookupEnvFunc = func(key string) (string, bool) { return "", false }
	cfg, err := LoadMCPConfig(dir)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if cfg.MCPServers["svc"].URL != "http://fallback:8080" {
		t.Errorf("url = %q, want default %q", cfg.MCPServers["svc"].URL, "http://fallback:8080")
	}

	// Test with env var set
	lookupEnvFunc = func(key string) (string, bool) {
		if key == "TEST_MCP_URL" {
			return "http://real:9090", true
		}
		return "", false
	}
	cfg, err = LoadMCPConfig(dir)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if cfg.MCPServers["svc"].URL != "http://real:9090" {
		t.Errorf("url = %q, want %q", cfg.MCPServers["svc"].URL, "http://real:9090")
	}
}

func TestLoadMCPConfig_ValidationErrors(t *testing.T) {
	origDir := userConfigDir
	userConfigDir = func() string { return "" }
	defer func() { userConfigDir = origDir }()

	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "stdio missing command",
			json:    `{"mcpServers":{"svc":{"type":"stdio"}}}`,
			wantErr: "requires 'command'",
		},
		{
			name:    "http missing url",
			json:    `{"mcpServers":{"svc":{"type":"http"}}}`,
			wantErr: "requires 'url'",
		},
		{
			name:    "unknown type",
			json:    `{"mcpServers":{"svc":{"type":"grpc","command":"x"}}}`,
			wantErr: "unknown type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(tt.json), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadMCPConfig(dir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadMCPConfig_NoConfigFiles(t *testing.T) {
	origDir := userConfigDir
	userConfigDir = func() string { return "" }
	defer func() { userConfigDir = origDir }()

	cfg, err := LoadMCPConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

func TestLoadMCPConfig_DefaultTypeIsStdio(t *testing.T) {
	dir := t.TempDir()
	// type omitted — should default to stdio and validate command
	data := `{"mcpServers":{"svc":{"command":"my-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir := userConfigDir
	userConfigDir = func() string { return "" }
	defer func() { userConfigDir = origDir }()

	cfg, err := LoadMCPConfig(dir)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	if cfg.MCPServers["svc"].Command != "my-server" {
		t.Errorf("command = %q, want %q", cfg.MCPServers["svc"].Command, "my-server")
	}
}

