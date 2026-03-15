# Smarter Context Compression

**Date:** 2026-03-14
**Status:** Approved

Improve compaction quality so the agent retains key decisions, file changes, and task state after compression. Reduce cost by compacting earlier with a cheaper model and compressing in stages.

---

## Change 1: Better Summary Prompt

Replace the current generic summarization prompt with one focused on what the agent needs to continue working.

Current prompt asks for: topics, files, tools, decisions, state.

New prompt asks for:
- **Decisions made** — what was agreed and why, so the agent does not revisit them
- **Files changed** — path and what changed, so the agent does not re-edit
- **Current plan** — what is in progress, what is next
- **Key constraints** — user preferences, things to avoid, project patterns
- **Errors encountered** — what failed and how it was resolved

The prompt explicitly says: "This summary will replace the conversation history. The agent must be able to continue the task using only this summary and the last 20 messages."

---

## Change 2: Tiered Compression

Instead of one cut at 100k tokens, compress progressively.

### Tier 1 (at 60k tokens): Tool result compression

Collapse tool results older than the tail (last 20 messages) to one-line summaries. Keep tool names and outcomes, drop raw output.

Format for each tool type:
- `file_read` -> `[read <path>: <N> lines]`
- `file_write` -> `[wrote <path>: <N> bytes]`
- `file_edit` -> `[edited <path>: replaced <N> occurrences]`
- `bash` -> `[bash: <first 80 chars of command> -> exit <code>]`
- `grep` -> `[grep <pattern>: <N> matches]`
- `glob` -> `[glob <pattern>: <N> files]`
- `web_fetch` -> `[fetched <url>: <N> chars]`
- Other tools -> `[<tool_name>: <first 100 chars of result>]`

This alone can cut 40-60% of context since tool results are the biggest token consumers.

### Tier 2 (at 75k tokens): Turn compression

Collapse old user/assistant exchanges (outside the tail) to bullet point summaries. Keep the last 20 messages intact.

Each collapsed turn becomes:
```
[Turn N: user asked to refactor auth -> agent edited auth.go, middleware.go, added tests]
```

### Tier 3 (at 90k tokens): Full summarization

The existing flow: serialize dropped messages, send to LLM for structured summary. But now with the better prompt from Change 1, and less work to do because Tiers 1-2 already compressed the bulk.

---

## Change 3: Lower Threshold and Cheaper Default

- Tier 1 triggers at 60k tokens (tool compression, no LLM call)
- Tier 2 triggers at 75k tokens (turn compression, no LLM call)
- Tier 3 triggers at 90k tokens (LLM summarization)
- Default `model.compact` to the cheapest available model in the current provider (e.g., `haiku` for Anthropic, `gpt-4o-mini` for OpenAI) if not explicitly set
- The user can still override with `/config set model.compact <model>`

---

## Implementation

### Modified files

| File | Change |
|------|--------|
| `internal/agent/compact.go` | New tiered compression functions, improved summary prompt, tool result summarizer |
| `internal/agent/compact_test.go` | Tests for each tier |
| `internal/provider/aliases.go` | Add `CheapModel(providerName string) string` helper for default compact model |

### New functions in compact.go

**`compressTier1(msgs []TranscriptMessage, tailStart int) []TranscriptMessage`**

Walks messages before `tailStart`. For each `tool_result` block, replaces the content with a one-line summary using `summarizeToolResult()`. Returns a new slice with modified messages. Original messages are not mutated.

**`compressTier2(msgs []TranscriptMessage, tailStart int) []TranscriptMessage`**

Walks messages before `tailStart`. Groups consecutive user+assistant pairs into single summary lines. Returns a compressed message list where each pair becomes one user message with a bullet point summary.

**`summarizeToolResult(toolName string, result string) string`**

Produces a one-line summary based on the tool name. Parses the result to extract key info (line count for file_read, byte count for file_write, exit code for bash, match count for grep, etc.). Falls back to first 100 chars for unknown tools.

**Modified `compactIfNeeded`**

Instead of one threshold check, runs through tiers in order:

```
if lastInputTokens > 60_000 and tier1 not yet applied:
    messages = compressTier1(messages, tailStart)
    emit CompactedEvent("tier1")

if lastInputTokens > 75_000 and tier2 not yet applied:
    messages = compressTier2(messages, tailStart)
    emit CompactedEvent("tier2")

if lastInputTokens > 90_000:
    run full LLM summarization (existing Tier 3 flow with new prompt)
    emit CompactedEvent("tier3")
```

Track which tiers have been applied with boolean flags on the Service: `tier1Applied`, `tier2Applied`.

**New `compactSummaryPrompt` constant**

```
You are summarizing a coding conversation. This summary will replace the conversation
history. The agent must be able to continue the current task using only this summary
and the most recent messages.

Produce a structured summary with these sections:

## Decisions made
What was agreed and why. Include specific technical choices so they are not revisited.

## Files changed
Every file path that was created, modified, or deleted, and a one-line description of what changed.

## Current plan
What is being worked on right now and what comes next.

## Key constraints
User preferences, patterns to follow, things to avoid.

## Errors encountered
What went wrong and how it was resolved. Include error messages if they might recur.

Be specific. Use exact file paths, function names, and variable names.
Maximum 600 words.
```

### Default cheap model

In `internal/provider/aliases.go`, add:

```go
func CheapModel(providerName string) string {
    switch providerName {
    case "anthropic":
        return "claude-haiku-4-5-20251001"
    case "openai":
        return "gpt-4o-mini"
    case "mistral":
        return "mistral-small-latest"
    case "grok":
        return "grok-3-mini"
    default:
        return "" // fall back to main model
    }
}
```

In `compact.go`, `summarizationModel()` changes:

```go
func (a *Service) summarizationModel() string {
    if a.modelCompact != "" {
        return a.modelCompact
    }
    if cheap := provider.CheapModel(a.provider.Name()); cheap != "" {
        return cheap
    }
    return a.modelID
}
```

---

## Testing Strategy

- **Tier 1 tests:** Create messages with large tool results, run `compressTier1`, verify results are collapsed to one-line summaries while tail messages are untouched.
- **Tier 2 tests:** Create a long conversation, run `compressTier2`, verify old turns are collapsed to bullet points while tail is preserved.
- **Tier 3 tests:** Existing tests still pass with the new prompt. Add a test verifying the prompt includes the required sections.
- **summarizeToolResult tests:** Table-driven for each tool type. Verify output format matches spec.
- **Integration:** Verify tiers are applied in order and flags prevent double-application.

---

## What is NOT in scope

- Changing the tail size (stays at 20 messages)
- Changing the head preservation (first exchange stays intact)
- Multiple compaction rounds within a single turn
- Token estimation (still relies on provider-reported token counts)
- Compaction UI changes (existing "compacted" event in TUI is sufficient)
