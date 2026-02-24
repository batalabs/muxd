package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MCPConfig holds MCP server configuration loaded from .mcp.json files.
type MCPConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig describes how to connect to a single MCP server.
type ServerConfig struct {
	Type    string            `json:"type"`              // "stdio" or "http"
	Command string            `json:"command,omitempty"` // stdio: executable
	Args    []string          `json:"args,omitempty"`    // stdio: arguments
	Env     map[string]string `json:"env,omitempty"`     // stdio: env vars
	URL     string            `json:"url,omitempty"`     // http: server URL
}

// userConfigDir returns the user-scope MCP config directory.
// Defaults to ~/.config/muxd/.
var userConfigDir = defaultUserConfigDir

func defaultUserConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "muxd")
}

// LoadMCPConfig loads and merges MCP configuration from project and user scope.
// Project-scoped (.mcp.json in cwd) overrides user-scoped (~/.config/muxd/mcp.json).
func LoadMCPConfig(cwd string) (MCPConfig, error) {
	merged := MCPConfig{MCPServers: map[string]ServerConfig{}}

	// 1. User scope
	userDir := userConfigDir()
	if userDir != "" {
		userPath := filepath.Join(userDir, "mcp.json")
		if cfg, err := loadConfigFile(userPath); err == nil {
			for name, sc := range cfg.MCPServers {
				merged.MCPServers[name] = sc
			}
		}
	}

	// 2. Project scope (overrides user)
	if cwd != "" {
		projectPath := filepath.Join(cwd, ".mcp.json")
		if cfg, err := loadConfigFile(projectPath); err == nil {
			for name, sc := range cfg.MCPServers {
				merged.MCPServers[name] = sc
			}
		}
	}

	// 3. Expand env vars and validate
	for name, sc := range merged.MCPServers {
		sc.Command = expandEnvVars(sc.Command)
		sc.URL = expandEnvVars(sc.URL)
		for i, arg := range sc.Args {
			sc.Args[i] = expandEnvVars(arg)
		}
		for k, v := range sc.Env {
			sc.Env[k] = expandEnvVars(v)
		}
		if err := validateServerConfig(name, sc); err != nil {
			return MCPConfig{}, err
		}
		merged.MCPServers[name] = sc
	}

	return merged, nil
}

func loadConfigFile(path string) (MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MCPConfig{}, err
	}
	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return MCPConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]ServerConfig{}
	}
	return cfg, nil
}

func validateServerConfig(name string, sc ServerConfig) error {
	switch sc.Type {
	case "stdio", "":
		if sc.Command == "" {
			return fmt.Errorf("MCP server %q: stdio type requires 'command'", name)
		}
	case "http":
		if sc.URL == "" {
			return fmt.Errorf("MCP server %q: http type requires 'url'", name)
		}
	default:
		return fmt.Errorf("MCP server %q: unknown type %q (expected 'stdio' or 'http')", name, sc.Type)
	}
	return nil
}

// envVarPattern matches ${VAR} and ${VAR:-default}.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// lookupEnvFunc returns (value, exists) for an environment variable.
// Override in tests to control the environment.
var lookupEnvFunc = os.LookupEnv

// expandEnvVars replaces ${VAR} and ${VAR:-default} patterns with values.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := envVarPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		varName := groups[1]
		defaultVal := ""
		if len(groups) >= 3 {
			defaultVal = groups[2]
		}
		val, exists := lookupEnvFunc(varName)
		if exists {
			return val
		}
		return strings.TrimSpace(defaultVal)
	})
}
