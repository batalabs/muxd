# Second Opinion / Consult

**Date:** 2026-03-14
**Status:** Approved

The agent can ask a different configured model for a second opinion when uncertain, and the user can trigger it manually with `/consult`. The response is shown in a separate styled view with a crystal ball emoji, independent from the main conversation.

---

## How It Works

**Agent-initiated:** The agent has a `consult` tool. When uncertain about a diagnosis, approach, or fix, it writes a focused consult request describing the problem, what it tried, and what it is unsure about. The request is sent to the consult model. The response is displayed in a separate styled block in the TUI. The primary agent does not see the response.

**User-initiated:** The user types `/consult` optionally followed by a specific question. muxd takes the last user message and agent response, builds a consult request, and sends it to the consult model. The response appears in the same separate view.

---

## Configuration

New preference: `model.consult` (string). No default. Must be explicitly configured.

- Set via `/config set model.consult claude-sonnet` or through the config picker (models group)
- Can be any model from any configured provider: `anthropic/claude-haiku`, `openai/gpt-4o`, etc.
- If not set, both the tool and `/consult` return a clear error: "No consult model configured. Use /config set model.consult <model>"

---

## Consult Tool

```
tool: consult
description: Ask a different AI model for a second opinion on a problem or approach.
parameters:
  summary: (string, required) A focused description of the problem, what you tried,
           and what you are uncertain about. Do NOT send raw code — summarize.
```

The tool:
1. Resolves `model.consult` from config. Errors if not set.
2. Resolves the provider and API key for the consult model (same resolution as the primary model).
3. Sends a single-turn request with system prompt: "You are providing a second opinion on a coding problem. Be concise and direct. Agree or disagree with the approach and explain why." User message is the summary.
4. The tool result returned to the primary agent is a brief acknowledgment: "Second opinion delivered to user." The actual consult response is sent to the TUI via a `ConsultResponseMsg` event, not fed back to the agent.

---

## /consult Slash Command

`/consult [optional question]`

If a question is provided, that becomes the consult request. If no question, muxd auto-builds a request from the last user and assistant exchange: "The user asked: <last user message>. The agent responded: <first 500 chars of last response>. What do you think?"

Same model resolution and display as the tool path.

---

## TUI Display

The consult response is shown as a distinct block in the scrollback:

```
🔮 Second Opinion (gpt-4o)
─────────────────────────
The approach looks correct. However, I'd suggest using
a sync.RWMutex instead of sync.Mutex here since the read
path is much more frequent than writes...
─────────────────────────
```

Uses a crystal ball emoji and a dimmed border to visually separate it from the main conversation. The model name is shown so the user knows which model responded.

---

## Implementation

### New files

| File | Responsibility |
|------|---------------|
| `internal/agent/consult.go` | `Consult(summary string) (string, error)` resolves model, makes single-turn API call, returns response text |
| `internal/agent/consult_test.go` | Tests for consult flow, missing config error |
| `internal/tools/consult_tool.go` | `consult` tool definition |

### Modified files

| File | Change |
|------|--------|
| `internal/config/preferences.go` | Add `ModelConsult string` field with JSON tag `model_consult`, add to models config group, add Get/Set cases |
| `internal/tools/tools.go` | Add `ConsultFunc func(summary string) (string, error)` to `ToolContext` |
| `internal/agent/agent.go` | Add `modelConsult string` field, `SetModelConsult` setter |
| `internal/agent/submit.go` | Wire `ConsultFunc` into `ToolContext` from agent fields |
| `internal/daemon/server.go` | Pass consult model config through to agent via `configureAgent` |
| `internal/tui/model.go` | New `ConsultResponseMsg` type, handler that renders styled block, `/consult` slash command |
| `internal/tui/render.go` | `FormatConsultResponse(model, text string, width int) string` rendering function |

### Architecture

The `Consult` function lives on the `agent.Service`. It:
1. Reads `modelConsult` field (set from config `model.consult`)
2. Calls `provider.ResolveProviderAndModel(modelConsult, "")` to get provider name and model ID
3. Loads the API key via `config.LoadProviderAPIKey`
4. Gets the provider via `provider.GetProvider`
5. Calls `provider.StreamMessage` with a single user message (the summary) and a system prompt
6. Collects the full response text
7. Returns it

The tool's `Execute` function calls `ctx.ConsultFunc(summary)`, sends a `ConsultResponseMsg` to `tui.Prog`, and returns "Second opinion delivered to user." to the agent.

The `/consult` slash command in the TUI fires a `tea.Cmd` that calls the daemon's consult endpoint (or the embedded agent's consult function directly), then sends `ConsultResponseMsg` on completion.

---

## Event Flow

### Agent-initiated

1. Agent calls `consult` tool with summary
2. Tool calls `ctx.ConsultFunc(summary)`
3. Consult function resolves provider, makes API call, gets response
4. Tool sends `ConsultResponseMsg{Model: "gpt-4o", Text: "..."}` to TUI via `tui.Prog.Send()`
5. Tool returns "Second opinion delivered to user." to agent
6. TUI renders styled consult block in scrollback

### User-initiated

1. User types `/consult why did the test fail?`
2. TUI builds consult request (user question or auto-summary of last exchange)
3. Fires a `tea.Cmd` that calls consult function
4. Response arrives as `ConsultResponseMsg`
5. TUI renders styled block

---

## Security

- Consult requests are single-turn, no tools. The consult model cannot execute code or modify files.
- The consult model's API key is resolved through the same config system as the primary model.
- No conversation history is sent. Only the focused summary.
- The consult response is not fed back to the primary agent.

---

## Testing Strategy

- **consult.go tests:** Mock provider that returns canned response. Test: successful consult, missing model config error, provider resolution error, empty response handling.
- **consult_tool.go tests:** Verify tool calls ConsultFunc, returns acknowledgment, handles nil ConsultFunc.
- **TUI tests:** Verify `FormatConsultResponse` output includes emoji, model name, and response text.
- **Config tests:** Verify `model.consult` appears in models group, Get/Set work correctly.

---

## What is NOT in scope

- Feeding the consult response back to the primary agent
- Multi-model consensus (asking three or more models)
- Consult model having access to tools or conversation history
- Automatic triggering (agent must explicitly call the tool)
- Streaming the consult response (single-turn, wait for full response)
