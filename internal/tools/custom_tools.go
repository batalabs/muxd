package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// tool_create
// ---------------------------------------------------------------------------

func toolCreateDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "tool_create",
			Description: "Create a new custom tool backed by a shell command template. Parameters use {{name}} placeholders in the command string. Optionally persist the tool to disk so it survives session restarts.",
			Properties: map[string]provider.ToolProp{
				"name":        {Type: "string", Description: "Tool name (letters, digits, underscores; must start with a letter; max 64 chars)"},
				"description": {Type: "string", Description: "Human-readable description of what the tool does"},
				"command":     {Type: "string", Description: "Shell command template; use {{param_name}} for parameter substitution"},
				"parameters":  {Type: "object", Description: "Parameter definitions as an object mapping parameter names to {type, description} objects"},
				"required":    {Type: "array", Description: "List of required parameter names"},
				"persistent":  {Type: "boolean", Description: "If true, save the tool to disk so it is available in future sessions (default: false)"},
			},
			Required: []string{"name", "description", "command"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			name, ok := input["name"].(string)
			if !ok || name == "" {
				return "", fmt.Errorf("name is required")
			}
			description, ok := input["description"].(string)
			if !ok || description == "" {
				return "", fmt.Errorf("description is required")
			}
			command, ok := input["command"].(string)
			if !ok || command == "" {
				return "", fmt.Errorf("command is required")
			}

			params, err := parseToolProps(input["parameters"])
			if err != nil {
				return "", fmt.Errorf("parsing parameters: %w", err)
			}

			required, err := parseRequiredList(input["required"])
			if err != nil {
				return "", fmt.Errorf("parsing required: %w", err)
			}

			persistent := false
			if v, ok := input["persistent"].(bool); ok {
				persistent = v
			}

			def := &CustomToolDef{
				Name:        name,
				Description: description,
				Command:     command,
				Parameters:  params,
				Required:    required,
				Persistent:  persistent,
			}

			if ctx == nil || ctx.CustomTools == nil {
				return "", fmt.Errorf("custom tool registry not available")
			}

			if err := ctx.CustomTools.Register(def); err != nil {
				return "", fmt.Errorf("registering tool: %w", err)
			}

			if persistent {
				dir, err := CustomToolsDir()
				if err != nil {
					return "", fmt.Errorf("resolving tools dir: %w", err)
				}
				if err := SaveTool(dir, def); err != nil {
					return "", fmt.Errorf("saving tool: %w", err)
				}
				return fmt.Sprintf("Tool %q created and saved to disk.", name), nil
			}

			return fmt.Sprintf("Tool %q created (session-only).", name), nil
		},
	}
}

// ---------------------------------------------------------------------------
// tool_register
// ---------------------------------------------------------------------------

func toolRegisterDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "tool_register",
			Description: "Register a new custom tool backed by an existing script file. The script receives parameters as PARAM_NAME environment variables. Optionally persist the registration to disk.",
			Properties: map[string]provider.ToolProp{
				"name":        {Type: "string", Description: "Tool name (letters, digits, underscores; must start with a letter; max 64 chars)"},
				"description": {Type: "string", Description: "Human-readable description of what the tool does"},
				"script":      {Type: "string", Description: "Path to an existing script file to execute"},
				"parameters":  {Type: "object", Description: "Parameter definitions as an object mapping parameter names to {type, description} objects"},
				"required":    {Type: "array", Description: "List of required parameter names"},
				"persistent":  {Type: "boolean", Description: "If true, save the registration to disk so it is available in future sessions (default: false)"},
			},
			Required: []string{"name", "description", "script"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			name, ok := input["name"].(string)
			if !ok || name == "" {
				return "", fmt.Errorf("name is required")
			}
			description, ok := input["description"].(string)
			if !ok || description == "" {
				return "", fmt.Errorf("description is required")
			}
			script, ok := input["script"].(string)
			if !ok || script == "" {
				return "", fmt.Errorf("script is required")
			}

			if _, err := os.Stat(script); err != nil {
				return "", fmt.Errorf("script file %q not found: %w", script, err)
			}

			params, err := parseToolProps(input["parameters"])
			if err != nil {
				return "", fmt.Errorf("parsing parameters: %w", err)
			}

			required, err := parseRequiredList(input["required"])
			if err != nil {
				return "", fmt.Errorf("parsing required: %w", err)
			}

			persistent := false
			if v, ok := input["persistent"].(bool); ok {
				persistent = v
			}

			def := &CustomToolDef{
				Name:        name,
				Description: description,
				Script:      script,
				Parameters:  params,
				Required:    required,
				Persistent:  persistent,
			}

			if ctx == nil || ctx.CustomTools == nil {
				return "", fmt.Errorf("custom tool registry not available")
			}

			if err := ctx.CustomTools.Register(def); err != nil {
				return "", fmt.Errorf("registering tool: %w", err)
			}

			if persistent {
				dir, err := CustomToolsDir()
				if err != nil {
					return "", fmt.Errorf("resolving tools dir: %w", err)
				}
				if err := SaveTool(dir, def); err != nil {
					return "", fmt.Errorf("saving tool: %w", err)
				}
				return fmt.Sprintf("Tool %q registered and saved to disk.", name), nil
			}

			return fmt.Sprintf("Tool %q registered (session-only).", name), nil
		},
	}
}

// ---------------------------------------------------------------------------
// tool_list_custom
// ---------------------------------------------------------------------------

func toolListCustomDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "tool_list_custom",
			Description: "List all custom tools registered in the current session.",
			Properties:  map[string]provider.ToolProp{},
			Required:    []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.CustomTools == nil {
				return "No custom tools registered.", nil
			}

			all := ctx.CustomTools.All()
			if len(all) == 0 {
				return "No custom tools registered.", nil
			}

			var sb strings.Builder
			for _, def := range all {
				persistence := "session-only"
				if def.Persistent {
					persistence = "persistent"
				}
				fmt.Fprintf(&sb, "- %s [%s]\n  %s\n", def.Name, persistence, def.Description)
				if def.Command != "" {
					fmt.Fprintf(&sb, "  command: %s\n", def.Command)
				}
				if def.Script != "" {
					fmt.Fprintf(&sb, "  script: %s\n", def.Script)
				}
			}

			return strings.TrimRight(sb.String(), "\n"), nil
		},
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// parseToolProps converts a map[string]any (as received from LLM input) into
// map[string]provider.ToolProp by marshaling to JSON and back.
func parseToolProps(raw any) (map[string]provider.ToolProp, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshaling: %w", err)
	}
	var props map[string]provider.ToolProp
	if err := json.Unmarshal(data, &props); err != nil {
		return nil, fmt.Errorf("unmarshaling: %w", err)
	}
	return props, nil
}

// parseRequiredList converts a []any (as received from LLM input) into
// []string by marshaling to JSON and back.
func parseRequiredList(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshaling: %w", err)
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("unmarshaling: %w", err)
	}
	return list, nil
}
