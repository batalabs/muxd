package mcp

import "strings"

const mcpPrefix = "mcp__"

// NamespacedName returns a namespaced tool name: "mcp__servername__toolname".
// The server name is sanitized to contain only lowercase alphanumeric and hyphens.
func NamespacedName(serverName, toolName string) string {
	return mcpPrefix + sanitizeName(serverName) + "__" + toolName
}

// ParseNamespacedName splits a namespaced MCP tool name into server and tool parts.
// Returns ("", "", false) if the name is not a valid MCP tool name.
func ParseNamespacedName(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, mcpPrefix) {
		return "", "", false
	}
	rest := name[len(mcpPrefix):]
	idx := strings.Index(rest, "__")
	if idx <= 0 {
		return "", "", false
	}
	server = rest[:idx]
	tool = rest[idx+2:]
	if tool == "" {
		return "", "", false
	}
	return server, tool, true
}

// IsMCPTool reports whether the tool name has the mcp__ prefix.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, mcpPrefix)
}

// sanitizeName lowercases and replaces non-alphanumeric characters with hyphens.
func sanitizeName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
