# Self-Extending Tools

**Date:** 2026-03-14
**Status:** Approved

Let the agent create new tools on demand — either simple JSON-defined command templates or complex shell scripts. Tools are ephemeral by default (session-only) or optionally persistent at `~/.config/muxd/tools/`.

---

## New Built-in Tools

### `tool_create`

Single-step tool creation for simple command-template tools.

```json
{
  "name": "api_health",
  "description": "Check API endpoint health",
  "parameters": {"url": {"type": "string", "description": "Endpoint URL"}},
  "required": ["url"],
  "command": "curl -s -o /dev/null -w '%{http_code}' {{url}}",
  "persistent": false
}
```

The agent calls this once. The tool is immediately available in the current session. If `persistent: true`, a JSON definition file is written to `~/.config/muxd/tools/api_health.json`.

### `tool_register`

Registers a script file the agent already created via `file_write`.

```json
{
  "name": "run_migrations",
  "description": "Run database migrations with environment selection",
  "parameters": {"env": {"type": "string", "enum": ["dev", "staging", "prod"]}},
  "required": ["env"],
  "script": "~/.config/muxd/tools/run_migrations.sh",
  "persistent": true
}
```

For complex multi-line scripts the agent writes first with `file_write`, then registers with `tool_register`.

### `tool_list_custom`

Lists all custom tools (ephemeral + persistent) with their definitions. The agent uses this to see what's already available before creating duplicates. No parameters.

---

## Tool Definition Format

Persistent tools are stored as JSON in `~/.config/muxd/tools/<name>.json`.

### Command-template tools

```json
{
  "name": "api_health",
  "description": "Check API endpoint health",
  "parameters": {
    "url": { "type": "string", "description": "Endpoint URL" }
  },
  "required": ["url"],
  "command": "curl -s -o /dev/null -w '%{http_code}' {{url}}"
}
```

### Script-based tools

```json
{
  "name": "run_migrations",
  "description": "Run database migrations",
  "parameters": {
    "env": { "type": "string", "enum": ["dev", "staging", "prod"] }
  },
  "required": ["env"],
  "script": "run_migrations.sh"
}
```

Script files live alongside the JSON definitions in `~/.config/muxd/tools/`. The `script` field is relative to that directory.

---

## Execution

When a custom tool is called by the agent:

1. Look up the tool definition in the in-memory custom tool registry.
2. **Command tools:** Substitute `{{param}}` placeholders with the actual parameter values, then execute via the existing bash tool infrastructure (same timeout, same CWD, same process group management).
3. **Script tools:** Execute the script file with parameters passed as environment variables (`PARAM_NAME=value`).
4. Return stdout as the tool result. If the command exits non-zero, return stderr as an error result.

Parameter substitution for command tools uses simple `{{name}}` replacement. Values are shell-escaped to prevent injection.

---

## Lifecycle

### Ephemeral tools (`persistent: false`)

- Stored in an in-memory map on the agent service.
- Available for the rest of the session.
- Gone on daemon restart.

### Persistent tools (`persistent: true`)

- Written to `~/.config/muxd/tools/<name>.json`.
- For script-based tools, the script file is at `~/.config/muxd/tools/<name>.sh` (or whatever extension).
- Loaded on startup.

### Startup loading

On daemon/agent startup, `loadCustomTools()` scans `~/.config/muxd/tools/*.json`, parses each definition, and registers them as custom tools. Invalid definitions are logged and skipped.

### Tool visibility

Custom tools appear alongside built-in tools in the LLM's tool list. They can be disabled via the same `/tools` mechanism as built-in tools.

---

## Security

- Custom tool commands run through the same bash execution path as the `bash` tool — same permissions, same CWD, same timeout.
- If the `bash` tool is disabled (e.g., `safe` profile), custom tools that use bash are also blocked. Custom tools inherit the `bash` tool's enabled/disabled state.
- `tool_create` validates:
  - No duplicate names with built-in tools or MCP tools.
  - Name must be alphanumeric + underscores, 1-64 characters.
  - Command or script must be non-empty.
  - Parameters must have valid types (`string`, `integer`, `boolean`, `array`).
- `tool_register` validates:
  - Script file must exist and be readable.
  - Same name/parameter validation as `tool_create`.
- Persistent scripts are stored in the user config directory (`~/.config/muxd/tools/`), not in project directories. The user controls what is saved.
- Parameter values are shell-escaped before substitution into command templates to prevent command injection.

---

## Implementation

### New files

| File | Responsibility |
|------|---------------|
| `internal/tools/custom.go` | Custom tool registry, `tool_create`/`tool_register`/`tool_list_custom` tool definitions, execution engine, JSON loading/saving, parameter substitution |
| `internal/tools/custom_test.go` | Tests for creation, registration, execution, persistence, loading, validation, shell escaping |

### Modified files

| File | Change |
|------|--------|
| `internal/tools/tools.go` | `FindTool()` falls through to custom tool registry if not found in built-in tools. `AllToolSpecs()` includes custom tool specs. |
| `internal/agent/tools.go` | `ExecuteToolCall()` routes custom tool calls through the custom executor (or this happens transparently via `FindTool`). |
| `internal/agent/submit.go` | Pass custom tool specs to the provider alongside built-in + MCP specs. |

### Architecture

The `CustomToolRegistry` is a struct with a `sync.RWMutex` protecting a `map[string]*CustomToolDef`. It is created once on the agent service and passed to `ToolContext` so tools can register new tools at runtime.

```go
type CustomToolRegistry struct {
    mu    sync.RWMutex
    tools map[string]*CustomToolDef
}

type CustomToolDef struct {
    Name        string                      `json:"name"`
    Description string                      `json:"description"`
    Parameters  map[string]provider.ToolProp `json:"parameters"`
    Required    []string                    `json:"required"`
    Command     string                      `json:"command,omitempty"`     // {{param}} template
    Script      string                      `json:"script,omitempty"`     // path to script file
    Persistent  bool                        `json:"-"`                    // not stored in JSON
    Ephemeral   bool                        `json:"-"`                    // session-only
}
```

The registry provides:
- `Register(def *CustomToolDef) error` — validates and adds to map
- `Find(name string) *CustomToolDef` — lookup by name
- `All() []*CustomToolDef` — list all registered custom tools
- `Specs() []provider.ToolSpec` — convert all to ToolSpecs for the LLM
- `Execute(name string, input map[string]any, cwd string) (string, error)` — run the tool
- `LoadFromDir(dir string) error` — load persistent tools on startup
- `SaveTool(dir string, def *CustomToolDef) error` — persist a tool definition

---

## Testing Strategy

- **Unit tests:** Create tool, register tool, list tools, execute command template, execute script, parameter substitution, shell escaping, validation (bad names, duplicates, missing fields).
- **Persistence tests:** Save to temp dir, load from temp dir, verify round-trip.
- **Integration:** Custom tool appears in tool specs, can be called by the agent loop, disabled when bash is disabled.

---

## What's NOT in scope

- Go source compilation at runtime.
- Project-level tool directories (user-level only).
- Auto-detection of repeated patterns (agent proactively creating tools).
- Tool versioning or update mechanisms.
- Tool sharing between users or across hub nodes.
