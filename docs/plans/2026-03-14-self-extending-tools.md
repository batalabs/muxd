# Self-Extending Tools Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the agent create, register, and execute custom tools at runtime — simple command templates via `tool_create`, complex scripts via `file_write` + `tool_register`, with optional persistence to `~/.config/muxd/tools/`.

**Architecture:** A `CustomToolRegistry` holds custom tool definitions in a thread-safe map. Three new built-in tools (`tool_create`, `tool_register`, `tool_list_custom`) manage the registry. Custom tools execute commands/scripts through the existing bash infrastructure. On startup, persistent tools are loaded from `~/.config/muxd/tools/*.json`.

**Tech Stack:** Go stdlib only. No new dependencies. Uses existing `os/exec` bash execution, `encoding/json` for persistence, `text/template` or simple string replacement for parameter substitution.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/tools/custom.go` | `CustomToolRegistry` type, `Register()`, `Find()`, `All()`, `Specs()`, `Execute()`, `LoadFromDir()`, `SaveTool()`, parameter substitution, shell escaping |
| `internal/tools/custom_test.go` | All tests: creation, execution, persistence, validation, escaping |
| `internal/tools/custom_tools.go` | `tool_create`, `tool_register`, `tool_list_custom` ToolDef definitions |

### Modified Files

| File | Change |
|------|--------|
| `internal/tools/tools.go` | Add `CustomRegistry` field to `ToolContext`, modify `FindTool()` to fall through to custom registry, modify `AllToolSpecs()` to include custom specs |
| `internal/agent/submit.go` | Pass custom tool specs to provider alongside built-in + MCP specs |
| `internal/daemon/server.go` | Create `CustomToolRegistry` on server init, pass to agent via `ToolContext` |
| `main.go` | Create registry, call `LoadFromDir()` on startup, pass to daemon/agent |

---

## Chunk 1: Custom Tool Registry Core

### Task 1: CustomToolRegistry type and Register/Find/All

**Files:**
- Create: `internal/tools/custom.go`
- Create: `internal/tools/custom_test.go`

- [ ] **Step 1: Write failing tests for registry basics**

Create `internal/tools/custom_test.go`:

```go
package tools

import (
	"testing"
)

func TestCustomToolRegistry_RegisterAndFind(t *testing.T) {
	r := NewCustomToolRegistry()

	def := &CustomToolDef{
		Name:        "my_tool",
		Description: "A test tool",
		Command:     "echo hello",
	}
	if err := r.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := r.Find("my_tool")
	if got == nil {
		t.Fatal("expected to find registered tool")
	}
	if got.Name != "my_tool" {
		t.Errorf("expected name my_tool, got %s", got.Name)
	}
}

func TestCustomToolRegistry_FindUnknown(t *testing.T) {
	r := NewCustomToolRegistry()
	if r.Find("nonexistent") != nil {
		t.Error("expected nil for unknown tool")
	}
}

func TestCustomToolRegistry_All(t *testing.T) {
	r := NewCustomToolRegistry()
	r.Register(&CustomToolDef{Name: "a", Description: "tool a", Command: "echo a"})
	r.Register(&CustomToolDef{Name: "b", Description: "tool b", Command: "echo b"})

	all := r.All()
	if len(all) != 2 {
		t.Errorf("expected 2 tools, got %d", len(all))
	}
}

func TestCustomToolRegistry_RegisterValidation(t *testing.T) {
	r := NewCustomToolRegistry()

	tests := []struct {
		name    string
		def     *CustomToolDef
		wantErr bool
	}{
		{"empty name", &CustomToolDef{Name: "", Command: "echo"}, true},
		{"invalid chars", &CustomToolDef{Name: "my-tool!", Command: "echo"}, true},
		{"too long", &CustomToolDef{Name: string(make([]byte, 65)), Command: "echo"}, true},
		{"no command or script", &CustomToolDef{Name: "empty_tool"}, true},
		{"builtin conflict", &CustomToolDef{Name: "bash", Command: "echo"}, true},
		{"valid command", &CustomToolDef{Name: "good_tool", Description: "ok", Command: "echo hi"}, false},
		{"valid script", &CustomToolDef{Name: "script_tool", Description: "ok", Script: "/tmp/test.sh"}, false},
		{"duplicate", &CustomToolDef{Name: "good_tool", Command: "echo"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.Register(tt.def)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run TestCustomToolRegistry -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement CustomToolRegistry**

Create `internal/tools/custom.go`:

```go
package tools

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/batalabs/muxd/internal/provider"
)

var validToolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// CustomToolDef defines a user-created tool.
type CustomToolDef struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Parameters  map[string]provider.ToolProp `json:"parameters,omitempty"`
	Required    []string                    `json:"required,omitempty"`
	Command     string                      `json:"command,omitempty"`
	Script      string                      `json:"script,omitempty"`
	Persistent  bool                        `json:"-"`
}

// CustomToolRegistry holds custom tools created at runtime.
type CustomToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]*CustomToolDef
}

// NewCustomToolRegistry creates an empty registry.
func NewCustomToolRegistry() *CustomToolRegistry {
	return &CustomToolRegistry{tools: make(map[string]*CustomToolDef)}
}

// Register validates and adds a custom tool definition.
func (r *CustomToolRegistry) Register(def *CustomToolDef) error {
	if !validToolName.MatchString(def.Name) {
		return fmt.Errorf("invalid tool name %q: must be 1-64 alphanumeric/underscore chars, starting with a letter", def.Name)
	}
	if def.Command == "" && def.Script == "" {
		return fmt.Errorf("tool %q must have a command or script", def.Name)
	}

	// Check for conflicts with built-in tools.
	if FindTool(def.Name) != nil {
		return fmt.Errorf("tool %q conflicts with a built-in tool", def.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("custom tool %q already exists", def.Name)
	}
	r.tools[def.Name] = def
	return nil
}

// Find returns a custom tool by name, or nil if not found.
func (r *CustomToolRegistry) Find(name string) *CustomToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// All returns all registered custom tools.
func (r *CustomToolRegistry) All() []*CustomToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*CustomToolDef, 0, len(r.tools))
	for _, d := range r.tools {
		out = append(out, d)
	}
	return out
}

// Specs converts all custom tools to provider.ToolSpec for the LLM.
func (r *CustomToolRegistry) Specs() []provider.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]provider.ToolSpec, 0, len(r.tools))
	for _, d := range r.tools {
		specs = append(specs, d.ToSpec())
	}
	return specs
}

// ToSpec converts a CustomToolDef to a provider.ToolSpec.
func (d *CustomToolDef) ToSpec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        d.Name,
		Description: d.Description,
		Properties:  d.Parameters,
		Required:    d.Required,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tools/ -run TestCustomToolRegistry -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/custom.go internal/tools/custom_test.go
git commit -m "feat: add CustomToolRegistry with register, find, and validation"
```

---

### Task 2: Parameter substitution and Execute

**Files:**
- Modify: `internal/tools/custom.go`
- Modify: `internal/tools/custom_test.go`

- [ ] **Step 1: Write failing tests for execution**

Add to `custom_test.go`:

```go
func TestSubstituteParams(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		params  map[string]any
		want    string
	}{
		{"simple", "echo {{msg}}", map[string]any{"msg": "hello"}, "echo hello"},
		{"multiple", "curl {{url}} -H {{header}}", map[string]any{"url": "http://x.com", "header": "Auth: tok"}, "curl http://x.com -H 'Auth: tok'"},
		{"shell chars escaped", "echo {{msg}}", map[string]any{"msg": "hello; rm -rf /"}, "echo 'hello; rm -rf /'"},
		{"no params", "echo hi", map[string]any{}, "echo hi"},
		{"missing param", "echo {{msg}}", map[string]any{}, "echo {{msg}}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substituteParams(tt.tmpl, tt.params)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCustomToolRegistry_Execute(t *testing.T) {
	r := NewCustomToolRegistry()
	r.Register(&CustomToolDef{
		Name:    "greet",
		Description: "say hello",
		Command: "echo hello {{name}}",
		Parameters: map[string]provider.ToolProp{
			"name": {Type: "string", Description: "who to greet"},
		},
		Required: []string{"name"},
	})

	result, err := r.Execute("greet", map[string]any{"name": "world"}, ".")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello world\n" && result != "hello world" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestCustomToolRegistry_ExecuteUnknown(t *testing.T) {
	r := NewCustomToolRegistry()
	_, err := r.Execute("nope", nil, ".")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestSubstituteParams|TestCustomToolRegistry_Execute" -v`
Expected: FAIL

- [ ] **Step 3: Implement substituteParams and Execute**

Add to `custom.go`:

```go
import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const customToolTimeout = 30 * time.Second

// substituteParams replaces {{name}} placeholders with shell-escaped values.
func substituteParams(tmpl string, params map[string]any) string {
	result := tmpl
	for k, v := range params {
		val := fmt.Sprintf("%v", v)
		// Shell-escape if value contains special characters.
		if strings.ContainsAny(val, " \t\n;|&$`\"'\\(){}[]<>!#~*?") {
			val = "'" + strings.ReplaceAll(val, "'", "'\\''") + "'"
		}
		result = strings.ReplaceAll(result, "{{"+k+"}}", val)
	}
	return result
}

// Execute runs a custom tool by name with the given parameters.
func (r *CustomToolRegistry) Execute(name string, input map[string]any, cwd string) (string, error) {
	def := r.Find(name)
	if def == nil {
		return "", fmt.Errorf("custom tool %q not found", name)
	}

	var cmdStr string
	if def.Command != "" {
		cmdStr = substituteParams(def.Command, input)
	} else if def.Script != "" {
		cmdStr = def.Script
	} else {
		return "", fmt.Errorf("tool %q has no command or script", name)
	}

	shell := "sh"
	shellFlag := "-c"
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellFlag = "/C"
	}

	ctx, cancel := context.WithTimeout(context.Background(), customToolTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, shellFlag, cmdStr)
	cmd.Dir = cwd

	// For script tools, pass params as env vars.
	if def.Script != "" && input != nil {
		cmd.Env = append(os.Environ(), paramsToEnv(input)...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

// paramsToEnv converts tool parameters to environment variables.
// Keys are uppercased: "my_param" -> "PARAM_MY_PARAM=value"
func paramsToEnv(params map[string]any) []string {
	var env []string
	for k, v := range params {
		env = append(env, fmt.Sprintf("PARAM_%s=%v", strings.ToUpper(k), v))
	}
	return env
}
```

Add `"context"` and `"os"` to the import block.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestSubstituteParams|TestCustomToolRegistry_Execute" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/custom.go internal/tools/custom_test.go
git commit -m "feat: add parameter substitution and custom tool execution"
```

---

### Task 3: Persistence — SaveTool and LoadFromDir

**Files:**
- Modify: `internal/tools/custom.go`
- Modify: `internal/tools/custom_test.go`

- [ ] **Step 1: Write failing tests for persistence**

Add to `custom_test.go`:

```go
func TestCustomToolRegistry_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	// Save
	r1 := NewCustomToolRegistry()
	def := &CustomToolDef{
		Name:        "saved_tool",
		Description: "A persistent tool",
		Command:     "echo saved",
		Persistent:  true,
	}
	if err := r1.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r1.SaveTool(dir, def); err != nil {
		t.Fatalf("SaveTool: %v", err)
	}

	// Load into fresh registry
	r2 := NewCustomToolRegistry()
	if err := r2.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	got := r2.Find("saved_tool")
	if got == nil {
		t.Fatal("expected to find loaded tool")
	}
	if got.Command != "echo saved" {
		t.Errorf("expected command 'echo saved', got %q", got.Command)
	}
	if !got.Persistent {
		t.Error("loaded tool should be marked persistent")
	}
}

func TestCustomToolRegistry_LoadFromDir_empty(t *testing.T) {
	dir := t.TempDir()
	r := NewCustomToolRegistry()
	if err := r.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir on empty dir: %v", err)
	}
	if len(r.All()) != 0 {
		t.Error("expected no tools from empty dir")
	}
}

func TestCustomToolRegistry_LoadFromDir_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{invalid"), 0o644)

	r := NewCustomToolRegistry()
	// Should not error — skips invalid files
	if err := r.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir with invalid JSON: %v", err)
	}
	if len(r.All()) != 0 {
		t.Error("expected no tools from invalid JSON")
	}
}
```

Add `"os"`, `"path/filepath"` to test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestCustomToolRegistry_Save|TestCustomToolRegistry_Load" -v`
Expected: FAIL

- [ ] **Step 3: Implement SaveTool and LoadFromDir**

Add to `custom.go`:

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SaveTool writes a tool definition to a JSON file in the given directory.
func (r *CustomToolRegistry) SaveTool(dir string, def *CustomToolDef) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating tools dir: %w", err)
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tool: %w", err)
	}
	path := filepath.Join(dir, def.Name+".json")
	return os.WriteFile(path, data, 0o644)
}

// LoadFromDir scans a directory for *.json tool definitions and registers them.
// Invalid files are logged and skipped.
func (r *CustomToolRegistry) LoadFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading tools dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var def CustomToolDef
		if err := json.Unmarshal(data, &def); err != nil {
			continue
		}
		if def.Name == "" || (def.Command == "" && def.Script == "") {
			continue
		}
		def.Persistent = true
		// Resolve relative script paths.
		if def.Script != "" && !filepath.IsAbs(def.Script) {
			def.Script = filepath.Join(dir, def.Script)
		}
		r.Register(&def) // ignore duplicate errors on reload
	}
	return nil
}

// CustomToolsDir returns the path to the user-level custom tools directory.
func CustomToolsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "muxd", "tools"), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestCustomToolRegistry_Save|TestCustomToolRegistry_Load" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/custom.go internal/tools/custom_test.go
git commit -m "feat: add custom tool persistence (save/load from ~/.config/muxd/tools/)"
```

---

## Chunk 2: Built-in Tool Definitions and Integration

### Task 4: tool_create, tool_register, tool_list_custom definitions

**Files:**
- Create: `internal/tools/custom_tools.go`

- [ ] **Step 1: Implement the three tool definitions**

Create `internal/tools/custom_tools.go`:

```go
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

func toolCreateDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "tool_create",
			Description: "Create a new custom tool from a command template. The tool becomes immediately available. Use {{param_name}} placeholders in the command for parameter substitution. Set persistent to true to save across sessions.",
			Properties: map[string]provider.ToolProp{
				"name":        {Type: "string", Description: "Tool name (alphanumeric and underscores, 1-64 chars)"},
				"description": {Type: "string", Description: "What the tool does"},
				"command":     {Type: "string", Description: "Shell command template with {{param}} placeholders"},
				"parameters": {Type: "object", Description: "Parameter definitions as {name: {type, description}} object"},
				"required":   {Type: "array", Description: "List of required parameter names", Items: &provider.ToolProp{Type: "string"}},
				"persistent": {Type: "boolean", Description: "If true, save to ~/.config/muxd/tools/ for future sessions"},
			},
			Required: []string{"name", "description", "command"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			name, _ := input["name"].(string)
			desc, _ := input["description"].(string)
			command, _ := input["command"].(string)
			persistent, _ := input["persistent"].(bool)

			if ctx.CustomTools == nil {
				return "", fmt.Errorf("custom tool registry not available")
			}

			def := &CustomToolDef{
				Name:        name,
				Description: desc,
				Command:     command,
				Persistent:  persistent,
			}

			// Parse parameters if provided.
			if params, ok := input["parameters"]; ok {
				raw, _ := json.Marshal(params)
				var props map[string]provider.ToolProp
				if err := json.Unmarshal(raw, &props); err == nil {
					def.Parameters = props
				}
			}
			if req, ok := input["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						def.Required = append(def.Required, s)
					}
				}
			}

			if err := ctx.CustomTools.Register(def); err != nil {
				return "", err
			}

			if persistent {
				dir, err := CustomToolsDir()
				if err != nil {
					return "", fmt.Errorf("getting tools dir: %w", err)
				}
				if err := ctx.CustomTools.SaveTool(dir, def); err != nil {
					return "", fmt.Errorf("saving tool: %w", err)
				}
				return fmt.Sprintf("Tool %q created and saved to ~/.config/muxd/tools/%s.json", name, name), nil
			}
			return fmt.Sprintf("Tool %q created (session only)", name), nil
		},
	}
}

func toolRegisterDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "tool_register",
			Description: "Register an existing script file as a custom tool. Write the script first with file_write, then register it here. Parameters are passed as PARAM_NAME environment variables.",
			Properties: map[string]provider.ToolProp{
				"name":        {Type: "string", Description: "Tool name (alphanumeric and underscores, 1-64 chars)"},
				"description": {Type: "string", Description: "What the tool does"},
				"script":      {Type: "string", Description: "Absolute path to the script file"},
				"parameters":  {Type: "object", Description: "Parameter definitions as {name: {type, description}} object"},
				"required":    {Type: "array", Description: "List of required parameter names", Items: &provider.ToolProp{Type: "string"}},
				"persistent":  {Type: "boolean", Description: "If true, save definition to ~/.config/muxd/tools/ for future sessions"},
			},
			Required: []string{"name", "description", "script"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			name, _ := input["name"].(string)
			desc, _ := input["description"].(string)
			script, _ := input["script"].(string)
			persistent, _ := input["persistent"].(bool)

			if ctx.CustomTools == nil {
				return "", fmt.Errorf("custom tool registry not available")
			}

			// Verify script exists.
			if _, err := os.Stat(script); err != nil {
				return "", fmt.Errorf("script file not found: %w", err)
			}

			def := &CustomToolDef{
				Name:        name,
				Description: desc,
				Script:      script,
				Persistent:  persistent,
			}

			if params, ok := input["parameters"]; ok {
				raw, _ := json.Marshal(params)
				var props map[string]provider.ToolProp
				if err := json.Unmarshal(raw, &props); err == nil {
					def.Parameters = props
				}
			}
			if req, ok := input["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						def.Required = append(def.Required, s)
					}
				}
			}

			if err := ctx.CustomTools.Register(def); err != nil {
				return "", err
			}

			if persistent {
				dir, err := CustomToolsDir()
				if err != nil {
					return "", fmt.Errorf("getting tools dir: %w", err)
				}
				if err := ctx.CustomTools.SaveTool(dir, def); err != nil {
					return "", fmt.Errorf("saving tool: %w", err)
				}
				return fmt.Sprintf("Tool %q registered and saved to ~/.config/muxd/tools/%s.json", name, name), nil
			}
			return fmt.Sprintf("Tool %q registered (session only)", name), nil
		},
	}
}

func toolListCustomDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "tool_list_custom",
			Description: "List all custom tools (both ephemeral and persistent) with their definitions.",
			Properties:  map[string]provider.ToolProp{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx.CustomTools == nil {
				return "No custom tool registry available.", nil
			}

			all := ctx.CustomTools.All()
			if len(all) == 0 {
				return "No custom tools registered.", nil
			}

			var sb strings.Builder
			for _, d := range all {
				persistence := "ephemeral"
				if d.Persistent {
					persistence = "persistent"
				}
				fmt.Fprintf(&sb, "- %s (%s): %s\n", d.Name, persistence, d.Description)
				if d.Command != "" {
					fmt.Fprintf(&sb, "  command: %s\n", d.Command)
				}
				if d.Script != "" {
					fmt.Fprintf(&sb, "  script: %s\n", d.Script)
				}
			}
			return sb.String(), nil
		},
	}
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/tools/`
Expected: May fail because `ToolContext.CustomTools` doesn't exist yet — that's expected, will be wired in Task 5.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/custom_tools.go
git commit -m "feat: add tool_create, tool_register, tool_list_custom definitions"
```

---

### Task 5: Wire into ToolContext and tool registry

**Files:**
- Modify: `internal/tools/tools.go`

- [ ] **Step 1: Add CustomTools to ToolContext**

In `internal/tools/tools.go`, add to the `ToolContext` struct (around line 62-81):

```go
CustomTools *CustomToolRegistry // runtime custom tool registry
```

- [ ] **Step 2: Add custom tool defs to AllTools()**

In `AllTools()` (around line 100-132), append the three new tools:

```go
toolCreateDef(),
toolRegisterDef(),
toolListCustomDef(),
```

- [ ] **Step 3: Modify FindTool to check custom registry**

Currently `FindTool` only searches `AllTools()`. Add a second function or modify the agent's dispatch to also check custom tools. The cleanest approach: add a `FindToolWithCustom` function:

```go
// FindToolWithCustom searches built-in tools first, then the custom registry.
func FindToolWithCustom(name string, registry *CustomToolRegistry) *ToolDef {
	if t := FindTool(name); t != nil {
		return t
	}
	if registry == nil {
		return nil
	}
	def := registry.Find(name)
	if def == nil {
		return nil
	}
	// Wrap custom tool as a ToolDef.
	td := ToolDef{
		Spec: def.ToSpec(),
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			return registry.Execute(def.Name, input, ctx.Cwd)
		},
	}
	return &td
}
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/tools/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tools/tools.go
git commit -m "feat: wire custom tools into ToolContext and tool lookup"
```

---

### Task 6: Wire into agent dispatch and startup

**Files:**
- Modify: `internal/agent/tools.go`
- Modify: `internal/agent/submit.go`
- Modify: `internal/daemon/server.go`
- Modify: `main.go`

- [ ] **Step 1: Modify ExecuteToolCall to use FindToolWithCustom**

In `internal/agent/tools.go`, change the tool lookup from `tools.FindTool(name)` to `tools.FindToolWithCustom(name, ctx.CustomTools)`. Also check custom tools before returning "unknown tool" error.

- [ ] **Step 2: Include custom tool specs in the provider call**

In `internal/agent/submit.go`, where tool specs are built for the provider (around line 168-183), append custom tool specs:

```go
if ctx.CustomTools != nil {
	for _, spec := range ctx.CustomTools.Specs() {
		if !disabled[spec.Name] {
			toolSpecs = append(toolSpecs, spec)
		}
	}
}
```

- [ ] **Step 3: Block custom tools when bash is disabled**

In the `Execute` method of `CustomToolRegistry`, or in `ExecuteToolCall`, check if `bash` is in the disabled set. If so, return an error for custom tools since they execute shell commands.

- [ ] **Step 4: Create registry on daemon startup**

In `internal/daemon/server.go`, create a `CustomToolRegistry` on the server and load persistent tools:

```go
customRegistry := tools.NewCustomToolRegistry()
if dir, err := tools.CustomToolsDir(); err == nil {
	customRegistry.LoadFromDir(dir)
}
```

Pass it through to the agent service so it ends up in `ToolContext.CustomTools`.

- [ ] **Step 5: Same for main.go embedded server path**

In `main.go`, create the registry and load persistent tools before the agent factory. Pass it through the same way.

- [ ] **Step 6: Verify build and tests**

Run: `go build ./...`
Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agent/tools.go internal/agent/submit.go internal/daemon/server.go main.go
git commit -m "feat: wire custom tool registry into agent dispatch and daemon startup"
```

---

### Task 7: Exclude custom tool management from sub-agents

**Files:**
- Modify: `internal/tools/tools.go`

- [ ] **Step 1: Add tool_create, tool_register, tool_list_custom to sub-agent exclusion**

In `AllToolsForSubAgent()` and `AllToolSpecsForSubAgent()`, exclude `tool_create`, `tool_register`, and `tool_list_custom` from sub-agents. Sub-agents should not create tools — only the main agent should.

Add to the exclusion set (alongside `task`, `plan_enter`, `plan_exit`, `hub_dispatch`, `hub_discovery`):

```go
"tool_create": true,
"tool_register": true,
"tool_list_custom": true,
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/tools/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tools/tools.go
git commit -m "feat: exclude tool management from sub-agents"
```

---

### Task 8: Final integration test and cleanup

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: Clean

- [ ] **Step 3: Build**

Run: `go build -o muxd.exe .`
Expected: Clean build

- [ ] **Step 4: Manual smoke test**

Run muxd, ask the agent: "create a tool called check_port that runs `nc -zv localhost {{port}}` with a required port parameter"

Verify:
1. Agent calls `tool_create`
2. Tool is registered
3. Agent can then call `check_port` with a port number

- [ ] **Step 5: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: final cleanup for self-extending tools"
```
