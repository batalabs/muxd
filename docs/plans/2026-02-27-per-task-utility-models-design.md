# Per-Task Utility Models

## Problem

muxd uses a single model for everything: user conversations, context compaction, auto-titling, and tag generation. Internal tasks like compaction summaries don't need the user's expensive primary model. The compaction code already hardcodes Haiku/GPT-4o-mini, but there's no way for users to configure which model handles each internal task.

## Design

Add per-task model configuration keys so users can assign a specific (typically cheaper) model to each internal task.

### Config Keys

| Key | Used for | Default (Anthropic) | Default (OpenAI) | Default (other) |
|-----|----------|---------------------|-------------------|-----------------|
| `model.compact` | Context compaction summaries | `claude-haiku-4-5-20251001` | `gpt-4o-mini` | main model |
| `model.title` | Auto-generated session titles | `claude-haiku-4-5-20251001` | `gpt-4o-mini` | main model |
| `model.tags` | Auto-generated session tags | `claude-haiku-4-5-20251001` | `gpt-4o-mini` | main model |

### Resolution Order

For each internal task:

1. Check the per-task config key (e.g. `model.compact`)
2. If unset, use provider-specific cheap default (Haiku for Anthropic, GPT-4o-mini for OpenAI)
3. If provider has no known cheap default, fall back to the user's main model

### Current State

- **Compaction** (`internal/agent/compact.go`): `summarizationModel()` already hardcodes Haiku/GPT-4o-mini per provider. This becomes configurable.
- **Auto-title** (`internal/agent/session.go`): Currently truncates the first user message (no LLM call). With `model.title` set, it could optionally call a cheap model for a better-quality title.
- **Tags** (`internal/agent/session.go`): `autoTag()` uses a cheap model call. With `model.tags` set, the model is configurable.

### Config Examples

```
/config set model.compact claude-haiku
/config set model.title gpt-4o-mini
/config set model.tags claude-haiku
```

Users who don't touch these keys get the same behavior as today (cheap defaults for Anthropic/OpenAI, main model for others).

## Files to Modify

| File | Change |
|------|--------|
| `internal/config/preferences.go` | Add `ModelCompact`, `ModelTitle`, `ModelTags` fields; wire into `Set()`, `Get()`, `All()`, `mergePreferences()`, config group defs |
| `internal/agent/agent.go` | Add `modelCompact`, `modelTitle`, `modelTags` fields to `Service` |
| `internal/agent/session.go` | Add setter methods; update `autoTitle()` and `autoTag()` to use per-task model |
| `internal/agent/compact.go` | Update `summarizationModel()` to read `modelCompact` field instead of hardcoding |
| `internal/agent/submit.go` | No change (main model path unchanged) |
| `internal/daemon/server.go` | Wire per-task model config into agent via `configureAgent()`; handle hot-reload in `handleSetConfig()` |
| `main.go` | Resolve per-task models at startup, pass to agent |

## Considerations

- Per-task models resolve through the same `ResolveProviderAndModel()` pipeline, so aliases work (`claude-haiku` resolves to `claude-haiku-4-5-20251001`).
- Per-task models can use a different provider than the main model (e.g. main model is Claude Opus, compaction uses GPT-4o-mini). This requires storing a provider + API key per task model, not just the model ID.
- Cross-provider utility models add complexity. **Simpler v1**: per-task models must use the same provider as the main model. If the user sets `model.compact gpt-4o-mini` while using Anthropic, validation rejects it with a clear error. Cross-provider support can come later.
- `/config models` should show which models are assigned to which tasks.
