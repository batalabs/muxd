# Per-Task Utility Models Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow users to configure which LLM model handles each internal task (compaction, title generation, tag generation) via per-task config keys, instead of hardcoding cheap models.

**Architecture:** Three new config keys (`model.compact`, `model.title`, `model.tags`) flow through the existing preferences -> agent -> daemon pipeline. Each resolves to a model ID using the same `ResolveProviderAndModel()` pipeline. V1 requires same-provider as the main model. The `summarizationModel()` method in compact.go becomes configurable, and `generateAndSetTitle()` gains optional LLM-based title generation.

**Tech Stack:** Go, SQLite (config persistence), Bubble Tea (TUI config display)

---

### Task 1: Add per-task model fields to Preferences

**Files:**
- Modify: `internal/config/preferences.go:16-53` (Preferences struct)
- Modify: `internal/config/preferences.go:74-95` (ConfigGroupDefs)
- Test: `internal/config/preferences_test.go`

**Step 1: Write the failing test**

Add to `internal/config/preferences_test.go`:

```go
func TestPerTaskModelConfig(t *testing.T) {
	t.Run("Set and Get model.compact", func(t *testing.T) {
		p := DefaultPreferences()
		if err := p.Set("model.compact", "claude-haiku"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Get("model.compact"); got != "claude-haiku" {
			t.Errorf("expected claude-haiku, got %s", got)
		}
	})

	t.Run("Set and Get model.title", func(t *testing.T) {
		p := DefaultPreferences()
		if err := p.Set("model.title", "gpt-4o-mini"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Get("model.title"); got != "gpt-4o-mini" {
			t.Errorf("expected gpt-4o-mini, got %s", got)
		}
	})

	t.Run("Set and Get model.tags", func(t *testing.T) {
		p := DefaultPreferences()
		if err := p.Set("model.tags", "claude-haiku"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Get("model.tags"); got != "claude-haiku" {
			t.Errorf("expected claude-haiku, got %s", got)
		}
	})

	t.Run("appears in All()", func(t *testing.T) {
		p := DefaultPreferences()
		_ = p.Set("model.compact", "claude-haiku")
		found := false
		for _, e := range p.All() {
			if e.Key == "model.compact" && e.Value == "claude-haiku" {
				found = true
			}
		}
		if !found {
			t.Error("model.compact not found in All()")
		}
	})

	t.Run("appears in models config group", func(t *testing.T) {
		p := DefaultPreferences()
		_ = p.Set("model.compact", "claude-haiku")
		group := p.GroupByName("models")
		if group == nil {
			t.Fatal("models group not found")
		}
		found := false
		for _, e := range group.Entries {
			if e.Key == "model.compact" {
				found = true
			}
		}
		if !found {
			t.Error("model.compact not in models group")
		}
	})
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestPerTaskModelConfig ./internal/config/ -v`
Expected: FAIL — `Set()` returns "unknown key: model.compact"

**Step 3: Write minimal implementation**

Add three fields to the `Preferences` struct (after `Model` on line 23):

```go
Model             string `json:"model"`
ModelCompact      string `json:"model_compact,omitempty"`
ModelTitle        string `json:"model_title,omitempty"`
ModelTags         string `json:"model_tags,omitempty"`
```

Add the three keys to `ConfigGroupDefs` in the `models` group (line 77):

```go
Keys: []string{"model", "model.compact", "model.title", "model.tags", "anthropic.api_key", ...},
```

Add cases to `Set()` (after `case "model":` on line 502-503):

```go
case "model.compact":
	p.ModelCompact = value
case "model.title":
	p.ModelTitle = value
case "model.tags":
	p.ModelTags = value
```

Add cases to `Get()` (after `case "model":` on line 408-409):

```go
case "model.compact":
	return p.ModelCompact
case "model.title":
	return p.ModelTitle
case "model.tags":
	return p.ModelTags
```

Add entries to `All()` (after `{"model", p.Model}` on line 370):

```go
{"model.compact", p.ModelCompact},
{"model.title", p.ModelTitle},
{"model.tags", p.ModelTags},
```

Add to `mergePreferences()` (after `if src.Model != ""` block on line 186-188):

```go
if src.ModelCompact != "" {
	dst.ModelCompact = src.ModelCompact
}
if src.ModelTitle != "" {
	dst.ModelTitle = src.ModelTitle
}
if src.ModelTags != "" {
	dst.ModelTags = src.ModelTags
}
```

Add to `sanitizePreferences()` (after `sanitize(&p.Model)` on line 595):

```go
sanitize(&p.ModelCompact)
sanitize(&p.ModelTitle)
sanitize(&p.ModelTags)
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestPerTaskModelConfig ./internal/config/ -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./internal/config/ -v`
Expected: All pass (no regressions)

**Step 6: Commit**

```bash
git add internal/config/preferences.go internal/config/preferences_test.go
git commit -m "feat(config): add model.compact, model.title, model.tags preference keys"
```

---

### Task 2: Add per-task model fields to agent.Service

**Files:**
- Modify: `internal/agent/agent.go:105-162` (Service struct)
- Modify: `internal/agent/session.go` (add setter methods)
- Test: `internal/agent/session_test.go`

**Step 1: Write the failing test**

Add to `internal/agent/session_test.go`:

```go
func TestService_SetUtilityModels(t *testing.T) {
	t.Run("SetModelCompact stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelCompact("claude-haiku-4-5-20251001")
		if svc.modelCompact != "claude-haiku-4-5-20251001" {
			t.Errorf("expected claude-haiku-4-5-20251001, got %s", svc.modelCompact)
		}
	})

	t.Run("SetModelTitle stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelTitle("gpt-4o-mini")
		if svc.modelTitle != "gpt-4o-mini" {
			t.Errorf("expected gpt-4o-mini, got %s", svc.modelTitle)
		}
	})

	t.Run("SetModelTags stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelTags("claude-haiku-4-5-20251001")
		if svc.modelTags != "claude-haiku-4-5-20251001" {
			t.Errorf("expected claude-haiku-4-5-20251001, got %s", svc.modelTags)
		}
	})
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestService_SetUtilityModels ./internal/agent/ -v`
Expected: FAIL — `svc.modelCompact` undefined, `SetModelCompact` undefined

**Step 3: Write minimal implementation**

Add three fields to `Service` struct in `agent.go` (after `textbeltAPIKey` on line 146):

```go
textbeltAPIKey string

// Per-task utility model overrides (resolved model IDs).
modelCompact string // for compaction summaries
modelTitle   string // for auto-title generation
modelTags    string // for auto-tag generation
```

Add setter methods to `session.go` (after `SetTextbeltAPIKey`):

```go
// SetModelCompact sets the model ID for compaction summaries.
func (a *Service) SetModelCompact(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelCompact = id
}

// SetModelTitle sets the model ID for auto-title generation.
func (a *Service) SetModelTitle(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelTitle = id
}

// SetModelTags sets the model ID for auto-tag generation.
func (a *Service) SetModelTags(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelTags = id
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestService_SetUtilityModels ./internal/agent/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/session.go internal/agent/session_test.go
git commit -m "feat(agent): add per-task utility model fields and setters"
```

---

### Task 3: Make summarizationModel() configurable

**Files:**
- Modify: `internal/agent/compact.go:197-210` (summarizationModel)
- Test: `internal/agent/compact_test.go`

**Step 1: Write the failing test**

Add to `internal/agent/compact_test.go`:

```go
func TestService_summarizationModel(t *testing.T) {
	t.Run("returns modelCompact when set", func(t *testing.T) {
		svc := &Service{
			modelCompact: "custom-cheap-model",
			modelID:      "claude-opus-4-6",
			prov:         &mockProvider{name: "anthropic"},
		}
		if got := svc.summarizationModel(); got != "custom-cheap-model" {
			t.Errorf("expected custom-cheap-model, got %s", got)
		}
	})

	t.Run("falls back to haiku for anthropic", func(t *testing.T) {
		svc := &Service{
			modelID: "claude-opus-4-6",
			prov:    &mockProvider{name: "anthropic"},
		}
		if got := svc.summarizationModel(); got != "claude-haiku-4-5-20251001" {
			t.Errorf("expected claude-haiku-4-5-20251001, got %s", got)
		}
	})

	t.Run("falls back to gpt-4o-mini for openai", func(t *testing.T) {
		svc := &Service{
			modelID: "gpt-4o",
			prov:    &mockProvider{name: "openai"},
		}
		if got := svc.summarizationModel(); got != "gpt-4o-mini" {
			t.Errorf("expected gpt-4o-mini, got %s", got)
		}
	})

	t.Run("falls back to main model for unknown provider", func(t *testing.T) {
		svc := &Service{
			modelID: "my-custom-model",
			prov:    &mockProvider{name: "custom"},
		}
		if got := svc.summarizationModel(); got != "my-custom-model" {
			t.Errorf("expected my-custom-model, got %s", got)
		}
	})
}
```

Note: Check if `mockProvider` already exists in the test file. If not, add:

```go
type mockProvider struct {
	name string
}
func (m *mockProvider) Name() string { return m.name }
// ... implement other Provider interface methods as no-ops
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestService_summarizationModel ./internal/agent/ -v`
Expected: FAIL — "returns modelCompact when set" fails because current code ignores `modelCompact`

**Step 3: Write minimal implementation**

Update `summarizationModel()` in `compact.go` (replace lines 200-210):

```go
func (a *Service) summarizationModel() string {
	// User-configured override takes priority.
	if a.modelCompact != "" {
		return a.modelCompact
	}
	// Provider-specific cheap defaults.
	if a.prov != nil {
		switch a.prov.Name() {
		case "anthropic":
			return "claude-haiku-4-5-20251001"
		case "openai":
			return "gpt-4o-mini"
		}
	}
	return a.modelID
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestService_summarizationModel ./internal/agent/ -v`
Expected: PASS

**Step 5: Run full agent tests**

Run: `go test ./internal/agent/ -v`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/agent/compact.go internal/agent/compact_test.go
git commit -m "feat(agent): make compaction model configurable via model.compact"
```

---

### Task 4: Wire per-task models through daemon configureAgent

**Files:**
- Modify: `internal/daemon/server.go:1009-1052` (configureAgent)
- Modify: `internal/daemon/server.go` (handleSetConfig for hot-reload)

**Step 1: Update configureAgent**

Add after the existing `SetTextbeltAPIKey` block in `configureAgent()` (after line 1015):

```go
if s.prefs != nil && s.prefs.ModelCompact != "" {
	_, compactID := provider.ResolveProviderAndModel(s.prefs.ModelCompact, s.provider.Name())
	ag.SetModelCompact(compactID)
}
if s.prefs != nil && s.prefs.ModelTitle != "" {
	_, titleID := provider.ResolveProviderAndModel(s.prefs.ModelTitle, s.provider.Name())
	ag.SetModelTitle(titleID)
}
if s.prefs != nil && s.prefs.ModelTags != "" {
	_, tagsID := provider.ResolveProviderAndModel(s.prefs.ModelTags, s.provider.Name())
	ag.SetModelTags(tagsID)
}
```

**Step 2: Add hot-reload in handleSetConfig**

Find the `handleSetConfig` method and add cases for the three new keys. When `model.compact`, `model.title`, or `model.tags` changes, call the appropriate setter on active agents:

```go
case "model.compact":
	_, id := provider.ResolveProviderAndModel(value, s.provider.Name())
	for _, ag := range s.agents {
		ag.SetModelCompact(id)
	}
case "model.title":
	_, id := provider.ResolveProviderAndModel(value, s.provider.Name())
	for _, ag := range s.agents {
		ag.SetModelTitle(id)
	}
case "model.tags":
	_, id := provider.ResolveProviderAndModel(value, s.provider.Name())
	for _, ag := range s.agents {
		ag.SetModelTags(id)
	}
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: No errors

Run: `go vet ./...`
Expected: No warnings

**Step 4: Commit**

```bash
git add internal/daemon/server.go
git commit -m "feat(daemon): wire per-task utility model config into agent"
```

---

### Task 5: Verify full test suite and build

**Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: All pass

**Step 2: Run vet**

Run: `go vet ./...`
Expected: No warnings

**Step 3: Run build**

Run: `go build -o muxd.exe .`
Expected: Clean build

**Step 4: Manual smoke test**

```
# Start muxd
./muxd.exe

# Set a per-task model
/config set model.compact claude-haiku

# Verify it shows in config
/config models
```

Expected: `model.compact` shows `claude-haiku` in the models group

**Step 5: Final commit if any fixups needed**

```bash
git add -A
git commit -m "chore: fixups for per-task utility models"
```
