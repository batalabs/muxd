# Second Opinion / Consult Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `consult` tool and `/consult` slash command that sends a focused question to a different configured model and displays the response in a styled separate view with a crystal ball emoji.

**Architecture:** A `Consult` method on `agent.Service` resolves the consult model's provider and API key, makes a single-turn API call, and returns the response. The `consult` tool calls this via `ToolContext.ConsultFunc`, sends a `ConsultResponseMsg` to the TUI, and returns an acknowledgment to the agent. The `/consult` slash command fires the same flow from the TUI side. The consult response is never fed back to the primary agent.

**Tech Stack:** Go stdlib + existing provider infrastructure. No new dependencies.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/agent/consult.go` | `Consult(summary string) (string, error)` — resolve model, single-turn API call, return response |
| `internal/agent/consult_test.go` | Tests for consult flow |
| `internal/tools/consult_tool.go` | `consult` tool definition |

### Modified Files

| File | Change |
|------|--------|
| `internal/config/preferences.go` | Add `ModelConsult` field, models group key, Get/Set |
| `internal/tools/tools.go` | Add `ConsultFunc` to `ToolContext`, add `consultToolDef()` to `AllTools()` |
| `internal/agent/agent.go` | Add `modelConsult` field, `SetModelConsult` setter |
| `internal/agent/submit.go` | Wire `ConsultFunc` into `ToolContext` |
| `internal/daemon/server.go` | Pass consult model to agent in `configureAgent` |
| `internal/tui/model.go` | `ConsultResponseMsg` type, handler, `/consult` slash command |
| `internal/tui/render.go` | `FormatConsultResponse` rendering function |

---

## Chunk 1: Config and Core Consult Function

### Task 1: Add model.consult preference

**Files:**
- Modify: `internal/config/preferences.go`

- [ ] **Step 1: Add ModelConsult field to Preferences struct**

Add to the Preferences struct (near the other Model fields):

```go
ModelConsult string `json:"model_consult,omitempty"`
```

- [ ] **Step 2: Add to models config group**

In `ConfigGroupDefs`, add `"model.consult"` to the `models` group Keys slice.

- [ ] **Step 3: Add to All() method**

Add entry:
```go
{"model.consult", p.ModelConsult},
```

- [ ] **Step 4: Add Get case**

```go
case "model.consult":
    return p.ModelConsult
```

- [ ] **Step 5: Add Set case**

```go
case "model.consult":
    p.ModelConsult = value
```

- [ ] **Step 6: Verify build and tests**

Run: `go build ./...`
Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/preferences.go
git commit -m "feat: add model.consult preference"
```

---

### Task 2: Consult function on agent.Service

**Files:**
- Create: `internal/agent/consult.go`
- Create: `internal/agent/consult_test.go`
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add modelConsult field and setter to agent.go**

In `internal/agent/agent.go`, add to the `Service` struct:

```go
modelConsult string
```

Add setter:

```go
func (a *Service) SetModelConsult(model string) { a.modelConsult = model }
```

- [ ] **Step 2: Write failing test for Consult**

Create `internal/agent/consult_test.go`:

```go
package agent

import (
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

type mockConsultProvider struct {
	response string
	err      error
}

func (p *mockConsultProvider) Name() string { return "mock" }
func (p *mockConsultProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	return nil, nil
}
func (p *mockConsultProvider) StreamMessage(
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []provider.ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, provider.Usage, error) {
	if p.err != nil {
		return nil, "", provider.Usage{}, p.err
	}
	return []domain.ContentBlock{
		{Type: "text", Text: p.response},
	}, "end_turn", provider.Usage{}, nil
}

func TestConsult(t *testing.T) {
	t.Run("returns response from consult model", func(t *testing.T) {
		svc := &Service{
			modelConsult: "mock-model",
		}
		mock := &mockConsultProvider{response: "Use RWMutex instead."}

		result, err := svc.consultWithProvider(mock, "test-key", "mock-model", "Should I use Mutex or RWMutex?")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "RWMutex") {
			t.Errorf("expected response to contain RWMutex, got: %s", result)
		}
	})

	t.Run("errors when no consult model configured", func(t *testing.T) {
		svc := &Service{}
		_, err := svc.Consult("some question")
		if err == nil {
			t.Fatal("expected error when no consult model set")
		}
		if !strings.Contains(err.Error(), "No consult model configured") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestConsult -v`
Expected: FAIL — Consult/consultWithProvider not defined

- [ ] **Step 4: Implement consult.go**

Create `internal/agent/consult.go`:

```go
package agent

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

const consultSystemPrompt = "You are providing a second opinion on a coding problem. Be concise and direct. Agree or disagree with the approach and explain why. Keep your response under 300 words."

// Consult sends a question to the configured consult model and returns the response.
// Returns an error if model.consult is not configured.
func (a *Service) Consult(summary string) (string, error) {
	if a.modelConsult == "" {
		return "", fmt.Errorf("No consult model configured. Use /config set model.consult <model>")
	}

	providerName, modelID := provider.ResolveProviderAndModel(a.modelConsult, "")
	apiKey, _ := config.LoadProviderAPIKey(config.Preferences{}, providerName)

	// Try loading from the agent's own prefs if available.
	if apiKey == "" && a.apiKey != "" && providerName == a.provider.Name() {
		apiKey = a.apiKey
	}

	prov, err := provider.GetProvider(providerName)
	if err != nil {
		return "", fmt.Errorf("consult: provider %q: %w", providerName, err)
	}

	return a.consultWithProvider(prov, apiKey, modelID, summary)
}

// consultWithProvider is the testable core — takes an explicit provider.
func (a *Service) consultWithProvider(prov provider.Provider, apiKey, modelID, summary string) (string, error) {
	history := []domain.TranscriptMessage{
		{Role: "user", Content: summary},
	}

	var response strings.Builder
	blocks, _, _, err := prov.StreamMessage(
		apiKey, modelID,
		history,
		nil, // no tools
		consultSystemPrompt,
		func(delta string) {
			response.WriteString(delta)
		},
	)
	if err != nil {
		return "", fmt.Errorf("consult: %w", err)
	}

	// Prefer streamed text, fall back to blocks.
	result := response.String()
	if result == "" {
		for _, b := range blocks {
			if b.Type == "text" {
				result += b.Text
			}
		}
	}

	if result == "" {
		return "No response from consult model.", nil
	}
	return result, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/agent/ -run TestConsult -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/consult.go internal/agent/consult_test.go internal/agent/agent.go
git commit -m "feat: add Consult function for second opinion from another model"
```

---

## Chunk 2: Tool, TUI, and Wiring

### Task 3: consult tool definition

**Files:**
- Create: `internal/tools/consult_tool.go`
- Modify: `internal/tools/tools.go`

- [ ] **Step 1: Create consult_tool.go**

```go
package tools

import (
	"fmt"

	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/tui"
)

func consultToolDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "consult",
			Description: "Ask a different AI model for a second opinion on a problem or approach. Write a focused summary of the problem and your uncertainty. The response is shown to the user in a separate view.",
			Properties: map[string]provider.ToolProp{
				"summary": {Type: "string", Description: "A focused description of the problem, what you tried, and what you are uncertain about"},
			},
			Required: []string{"summary"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			summary, _ := input["summary"].(string)
			if summary == "" {
				return "", fmt.Errorf("summary is required")
			}

			if ctx.ConsultFunc == nil {
				return "", fmt.Errorf("No consult model configured. Use /config set model.consult <model>")
			}

			model, response, err := ctx.ConsultFunc(summary)
			if err != nil {
				return "", err
			}

			// Send response to TUI as a separate view.
			if tui.Prog != nil {
				tui.Prog.Send(tui.ConsultResponseMsg{
					Model: model,
					Text:  response,
				})
			}

			return "Second opinion delivered to user.", nil
		},
	}
}
```

- [ ] **Step 2: Add ConsultFunc to ToolContext**

In `internal/tools/tools.go`, add to `ToolContext`:

```go
ConsultFunc func(summary string) (model string, response string, err error)
```

- [ ] **Step 3: Add consultToolDef to AllTools**

In `AllTools()`, add `consultToolDef()` to the return slice.

- [ ] **Step 4: Add consult to sub-agent exclusion**

In `internal/tools/task.go`, add `"consult"` to `IsSubAgentTool`.

- [ ] **Step 5: Update tool count in tests**

Update `TestAllToolSpecs` expected count if it exists.

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: Will fail because `tui.ConsultResponseMsg` doesn't exist yet. That's OK — we'll add it in Task 4.

- [ ] **Step 7: Commit**

```bash
git add internal/tools/consult_tool.go internal/tools/tools.go internal/tools/task.go
git commit -m "feat: add consult tool definition"
```

---

### Task 4: TUI ConsultResponseMsg and rendering

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/render.go`

- [ ] **Step 1: Add ConsultResponseMsg type**

In `internal/tui/model.go`, add near the other message types (top of file):

```go
// ConsultResponseMsg delivers a second opinion from a different model.
type ConsultResponseMsg struct {
	Model string
	Text  string
}
```

- [ ] **Step 2: Add FormatConsultResponse to render.go**

In `internal/tui/render.go`:

```go
// FormatConsultResponse renders a second opinion response with crystal ball emoji.
func FormatConsultResponse(model, text string, width int) string {
	header := fmt.Sprintf("🔮 Second Opinion (%s)", model)
	divider := strings.Repeat("─", min(40, width-4))

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(FooterHead.Render(header))
	sb.WriteString("\n")
	sb.WriteString(FooterMeta.Render(divider))
	sb.WriteString("\n")

	// Render response text with word wrap.
	for _, line := range strings.Split(text, "\n") {
		if len(line) > width-4 && width > 8 {
			// Simple word wrap
			for len(line) > width-4 {
				cut := strings.LastIndex(line[:width-4], " ")
				if cut <= 0 {
					cut = width - 4
				}
				sb.WriteString(FooterMeta.Render(line[:cut]))
				sb.WriteString("\n")
				line = line[cut:]
				line = strings.TrimLeft(line, " ")
			}
		}
		sb.WriteString(FooterMeta.Render(line))
		sb.WriteString("\n")
	}

	sb.WriteString(FooterMeta.Render(divider))
	sb.WriteString("\n")
	return sb.String()
}
```

- [ ] **Step 3: Handle ConsultResponseMsg in Update**

In `model.go`, in the main `Update` switch (where other message types are handled), add:

```go
case ConsultResponseMsg:
	formatted := FormatConsultResponse(msg.Model, msg.Text, m.width)
	return m, PrintToScrollback(formatted)
```

- [ ] **Step 4: Implement /consult slash command**

In the slash command switch in `model.go` (where `/new`, `/config`, etc. are handled), add:

```go
case "/consult":
	if m.Daemon == nil {
		return m, PrintToScrollback(m.renderError("No daemon connection."))
	}
	question := strings.TrimSpace(strings.TrimPrefix(trimmed, "/consult"))
	if question == "" {
		// Auto-build from last exchange
		if len(m.messages) >= 2 {
			lastUser := ""
			lastAssistant := ""
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Role == "assistant" && lastAssistant == "" {
					lastAssistant = m.messages[i].Content
					if len(lastAssistant) > 500 {
						lastAssistant = lastAssistant[:500] + "..."
					}
				}
				if m.messages[i].Role == "user" && lastUser == "" {
					lastUser = m.messages[i].Content
				}
				if lastUser != "" && lastAssistant != "" {
					break
				}
			}
			question = fmt.Sprintf("The user asked: %s\n\nThe agent responded: %s\n\nWhat do you think?", lastUser, lastAssistant)
		} else {
			return m, PrintToScrollback(m.renderError("No conversation to consult about. Provide a question: /consult <question>"))
		}
	}
	return m, ConsultCmd(m.Daemon, m.Session.ID, question)
```

- [ ] **Step 5: Add ConsultCmd function**

Add near `StreamViaDaemon`:

```go
// ConsultCmd sends a consult request and delivers the response as a ConsultResponseMsg.
func ConsultCmd(d *daemon.DaemonClient, sessionID, question string) tea.Cmd {
	return func() tea.Msg {
		// For now, call the consult endpoint directly.
		// This will be wired through the daemon in Task 5.
		return ConsultResponseMsg{
			Model: "pending",
			Text:  "Consult not yet wired to daemon.",
		}
	}
}
```

This is a placeholder — Task 5 will wire it through the daemon.

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/render.go
git commit -m "feat: add ConsultResponseMsg, styled rendering, and /consult command"
```

---

### Task 5: Wire consult through agent and daemon

**Files:**
- Modify: `internal/agent/submit.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Wire ConsultFunc in submit.go**

In `submit.go`, where `ToolContext` is built (search for `toolCtx := &tools.ToolContext{`), add:

```go
ConsultFunc: func(summary string) (string, string, error) {
    response, err := a.Consult(summary)
    if err != nil {
        return "", "", err
    }
    return a.modelConsult, response, nil
},
```

- [ ] **Step 2: Pass modelConsult in configureAgent**

In `internal/daemon/server.go`, in `configureAgent`, add:

```go
if s.prefs != nil && s.prefs.ModelConsult != "" {
    ag.SetModelConsult(s.prefs.ModelConsult)
}
```

Read the existing `configureAgent` to find the right place — it should be near where `SetModelCompact` is called.

- [ ] **Step 3: Add consult endpoint to daemon server**

Add a new HTTP endpoint for the TUI's `/consult` command:

```go
mux.HandleFunc("POST /api/sessions/{id}/consult", s.withAuth(s.handleConsult))
```

Handler:

```go
func (s *Server) handleConsult(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("id")
    var req struct {
        Summary string `json:"summary"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
        return
    }

    ag, err := s.getOrCreateAgent(sessionID)
    if err != nil {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
        return
    }

    response, err := ag.Consult(req.Summary)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }

    writeJSON(w, http.StatusOK, map[string]string{
        "model":    ag.ConsultModel(),
        "response": response,
    })
}
```

Add `ConsultModel() string` getter to agent:

```go
func (a *Service) ConsultModel() string { return a.modelConsult }
```

- [ ] **Step 4: Add Consult method to DaemonClient**

In `internal/daemon/client.go`, add:

```go
func (c *DaemonClient) Consult(sessionID, summary string) (model, response string, err error) {
    body, _ := json.Marshal(map[string]string{"summary": summary})
    resp, err := c.post(fmt.Sprintf("/api/sessions/%s/consult", sessionID), body)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()
    var result struct {
        Model    string `json:"model"`
        Response string `json:"response"`
        Error    string `json:"error"`
    }
    json.NewDecoder(resp.Body).Decode(&result)
    if resp.StatusCode != http.StatusOK {
        return "", "", fmt.Errorf("%s", result.Error)
    }
    return result.Model, result.Response, nil
}
```

- [ ] **Step 5: Wire ConsultCmd in TUI**

Update the placeholder `ConsultCmd` in `model.go`:

```go
func ConsultCmd(d *daemon.DaemonClient, sessionID, question string) tea.Cmd {
    return func() tea.Msg {
        if d == nil {
            return ConsultResponseMsg{Model: "error", Text: "No daemon connection."}
        }
        model, response, err := d.Consult(sessionID, question)
        if err != nil {
            return ConsultResponseMsg{Model: "error", Text: err.Error()}
        }
        return ConsultResponseMsg{Model: model, Text: response}
    }
}
```

- [ ] **Step 6: Verify build and tests**

Run: `go build ./...`
Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agent/submit.go internal/agent/agent.go internal/daemon/server.go internal/daemon/client.go internal/tui/model.go
git commit -m "feat: wire consult through agent, daemon, and TUI"
```

---

### Task 6: Final integration test and cleanup

- [ ] **Step 1: Run go vet**

Run: `go vet ./...`
Expected: Clean

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 3: Build**

Run: `go build -o muxd.exe .`
Expected: Clean

- [ ] **Step 4: Update tool count on muxd.sh**

The tool count goes from 32 to 33. Update `app/page.tsx` and `app/docs/tools/page.mdx` on the muxd.sh repo.

- [ ] **Step 5: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: final cleanup for second opinion consult feature"
```
