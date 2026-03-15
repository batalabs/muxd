# Smarter Context Compression Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve compaction with tiered compression (tool results at 60k, turn summaries at 75k, LLM summary at 90k), a better summary prompt focused on decisions and file changes, and automatic cheap model selection for summarization.

**Architecture:** Three compression tiers run progressively in `compactIfNeeded`. Tier 1 and 2 are pure string manipulation (no LLM call). Tier 3 uses the existing LLM summarization flow with an improved prompt. A `CheapModel` helper selects the cheapest model per provider for Tier 3. Tier state is tracked with boolean flags on the Service struct.

**Tech Stack:** Go stdlib only. No new dependencies.

---

## File Structure

### Modified Files

| File | Change |
|------|--------|
| `internal/agent/compact.go` | New tiered functions, improved prompt, tool result summarizer, new thresholds, tier flags |
| `internal/agent/compact_test.go` | Tests for all three tiers, summarizeToolResult, new prompt |
| `internal/agent/agent.go` | Add `tier1Applied`, `tier2Applied` bool fields to Service struct |
| `internal/provider/aliases.go` | Add `CheapModel(providerName string) string` helper |

---

## Chunk 1: Tool Result Summarizer and Tier 1

### Task 1: summarizeToolResult function

**Files:**
- Modify: `internal/agent/compact.go`
- Modify: `internal/agent/compact_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/agent/compact_test.go`:

```go
func TestSummarizeToolResult(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		result   string
		wantHas  string
	}{
		{
			name:     "file_read shows line count",
			toolName: "file_read",
			result:   "   1 | package main\n   2 | \n   3 | func main() {\n",
			wantHas:  "[read",
		},
		{
			name:     "file_write shows bytes",
			toolName: "file_write",
			result:   "Wrote 1234 bytes (45 lines) to src/main.go",
			wantHas:  "[wrote",
		},
		{
			name:     "file_edit shows replacements",
			toolName: "file_edit",
			result:   "Replaced 2 occurrences in main.go (+10/-5 bytes)",
			wantHas:  "[edited",
		},
		{
			name:     "bash shows command and exit",
			toolName: "bash",
			result:   "PASS\nok  \tgithub.com/example\t0.5s\n",
			wantHas:  "[bash",
		},
		{
			name:     "grep shows match count",
			toolName: "grep",
			result:   "main.go:10:func main()\nmain.go:20:func helper()\n",
			wantHas:  "[grep",
		},
		{
			name:     "glob shows file count",
			toolName: "glob",
			result:   "src/a.go\nsrc/b.go\nsrc/c.go\n",
			wantHas:  "[glob",
		},
		{
			name:     "web_fetch shows char count",
			toolName: "web_fetch",
			result:   "Some fetched content that is longer than expected...",
			wantHas:  "[fetched",
		},
		{
			name:     "unknown tool truncates to 100 chars",
			toolName: "custom_tool",
			result:   strings.Repeat("x", 200),
			wantHas:  "[custom_tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeToolResult(tt.toolName, tt.result)
			if !strings.Contains(got, tt.wantHas) {
				t.Errorf("expected %q in summary, got %q", tt.wantHas, got)
			}
			// All summaries should be one line, under 200 chars
			if strings.Contains(got, "\n") {
				t.Errorf("summary should be single line, got: %q", got)
			}
			if len(got) > 200 {
				t.Errorf("summary too long: %d chars", len(got))
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestSummarizeToolResult -v`
Expected: FAIL — function not defined

- [ ] **Step 3: Implement summarizeToolResult**

Add to `internal/agent/compact.go`:

```go
// summarizeToolResult produces a one-line summary of a tool result
// based on the tool name. Used by Tier 1 compression.
func summarizeToolResult(toolName, result string) string {
	lines := strings.Split(strings.TrimSpace(result), "\n")
	lineCount := len(lines)

	switch toolName {
	case "file_read":
		return fmt.Sprintf("[read: %d lines]", lineCount)
	case "file_write":
		// Result format: "Wrote N bytes (M lines) to <path>"
		return fmt.Sprintf("[wrote: %s]", firstLine(result))
	case "file_edit":
		// Result format: "Replaced N occurrences in <path> ..."
		return fmt.Sprintf("[edited: %s]", firstLine(result))
	case "bash":
		first := firstLine(result)
		if len(first) > 80 {
			first = first[:80]
		}
		return fmt.Sprintf("[bash: %s]", first)
	case "grep":
		return fmt.Sprintf("[grep: %d matches]", lineCount)
	case "glob", "list_files":
		return fmt.Sprintf("[%s: %d files]", toolName, lineCount)
	case "web_fetch":
		return fmt.Sprintf("[fetched: %d chars]", len(result))
	case "web_search":
		return fmt.Sprintf("[search: %d results]", lineCount)
	default:
		s := strings.TrimSpace(result)
		if len(s) > 100 {
			s = s[:100] + "..."
		}
		return fmt.Sprintf("[%s: %s]", toolName, strings.ReplaceAll(s, "\n", " "))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestSummarizeToolResult -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/compact.go internal/agent/compact_test.go
git commit -m "feat: add summarizeToolResult for tiered compression"
```

---

### Task 2: Tier 1 compression — collapse tool results

**Files:**
- Modify: `internal/agent/compact.go`
- Modify: `internal/agent/compact_test.go`
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add tier flags to Service struct**

In `internal/agent/agent.go`, add to the `Service` struct:

```go
tier1Applied bool
tier2Applied bool
```

- [ ] **Step 2: Write failing tests for compressTier1**

Add to `compact_test.go`:

```go
func TestCompressTier1(t *testing.T) {
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: "read main.go"},
		{Role: "assistant", Content: "Sure", Blocks: []domain.ContentBlock{
			{Type: "tool_use", ToolName: "file_read"},
		}},
		{Role: "user", Blocks: []domain.ContentBlock{
			{Type: "tool_result", ToolName: "file_read", ToolResult: strings.Repeat("line\n", 100)},
		}},
		{Role: "assistant", Content: "I see the file has 100 lines."},
		// Tail messages (should not be compressed)
		{Role: "user", Content: "now edit it"},
		{Role: "assistant", Content: "Done."},
	}

	tailStart := 4 // last 2 messages are tail
	result := compressTier1(msgs, tailStart)

	if len(result) != len(msgs) {
		t.Fatalf("expected same message count, got %d", len(result))
	}

	// Tool result in message 2 (index 2) should be compressed
	compressed := result[2].Blocks[0].ToolResult
	if strings.Contains(compressed, "line\nline\n") {
		t.Error("tool result should be compressed, still has raw content")
	}
	if !strings.Contains(compressed, "[read") {
		t.Errorf("compressed result should contain [read, got: %s", compressed)
	}

	// Tail messages should be untouched
	if result[4].Content != "now edit it" {
		t.Error("tail message should not be modified")
	}
}

func TestCompressTier1_preservesTail(t *testing.T) {
	// Tool result IN the tail should not be compressed
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: "old message"},
		{Role: "assistant", Content: "old response"},
		{Role: "user", Blocks: []domain.ContentBlock{
			{Type: "tool_result", ToolName: "file_read", ToolResult: strings.Repeat("x", 1000)},
		}},
		{Role: "assistant", Content: "recent response"},
	}

	tailStart := 2 // last 2 are tail
	result := compressTier1(msgs, tailStart)

	// Tail tool result should still have full content
	if len(result[2].Blocks[0].ToolResult) < 1000 {
		t.Error("tail tool result should not be compressed")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestCompressTier1 -v`
Expected: FAIL — function not defined

- [ ] **Step 4: Implement compressTier1**

Add to `compact.go`:

```go
// compressTier1 collapses tool results outside the tail to one-line summaries.
// Messages are copied, not mutated in place.
func compressTier1(msgs []domain.TranscriptMessage, tailStart int) []domain.TranscriptMessage {
	out := make([]domain.TranscriptMessage, len(msgs))
	for i, m := range msgs {
		if i >= tailStart {
			out[i] = m
			continue
		}
		if len(m.Blocks) == 0 {
			out[i] = m
			continue
		}
		// Deep copy blocks so we don't mutate the original
		newBlocks := make([]domain.ContentBlock, len(m.Blocks))
		copy(newBlocks, m.Blocks)
		for j, b := range newBlocks {
			if b.Type == "tool_result" && len(b.ToolResult) > 200 {
				newBlocks[j].ToolResult = summarizeToolResult(b.ToolName, b.ToolResult)
			}
		}
		out[i] = m
		out[i].Blocks = newBlocks
	}
	return out
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/agent/ -run TestCompressTier1 -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/compact.go internal/agent/compact_test.go internal/agent/agent.go
git commit -m "feat: add Tier 1 compression — collapse tool results to summaries"
```

---

## Chunk 2: Tier 2, Tier 3, and Integration

### Task 3: Tier 2 compression — collapse old turns

**Files:**
- Modify: `internal/agent/compact.go`
- Modify: `internal/agent/compact_test.go`

- [ ] **Step 1: Write failing tests**

Add to `compact_test.go`:

```go
func TestCompressTier2(t *testing.T) {
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: "refactor the auth module"},
		{Role: "assistant", Content: "I'll start by reading the auth files and then restructure them."},
		{Role: "user", Content: "looks good, now add tests"},
		{Role: "assistant", Content: "I've added comprehensive tests for the auth module."},
		// Tail
		{Role: "user", Content: "recent question"},
		{Role: "assistant", Content: "recent answer"},
	}

	tailStart := 4
	result := compressTier2(msgs, tailStart)

	// Pre-tail messages should be collapsed
	if len(result) >= len(msgs) {
		t.Errorf("expected fewer messages after Tier 2, got %d (was %d)", len(result), len(msgs))
	}

	// Should contain summary text
	found := false
	for _, m := range result {
		if strings.Contains(m.Content, "refactor") || strings.Contains(m.Content, "auth") {
			found = true
			break
		}
	}
	if !found {
		t.Error("compressed turns should mention key topics")
	}

	// Tail should be intact
	lastTwo := result[len(result)-2:]
	if lastTwo[0].Content != "recent question" || lastTwo[1].Content != "recent answer" {
		t.Error("tail messages should be preserved exactly")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run TestCompressTier2 -v`
Expected: FAIL

- [ ] **Step 3: Implement compressTier2**

Add to `compact.go`:

```go
// compressTier2 collapses user+assistant turn pairs outside the tail
// into single-line bullet point summaries.
func compressTier2(msgs []domain.TranscriptMessage, tailStart int) []domain.TranscriptMessage {
	if tailStart <= 0 {
		return msgs
	}

	var summary strings.Builder
	summary.WriteString("[Compressed conversation history]\n\n")

	for i := 0; i < tailStart; i++ {
		m := msgs[i]
		switch m.Role {
		case "user":
			content := m.Content
			if content == "" {
				content = m.TextContent()
			}
			if len(content) > 120 {
				content = content[:120] + "..."
			}
			fmt.Fprintf(&summary, "- User: %s\n", strings.ReplaceAll(content, "\n", " "))
		case "assistant":
			content := m.Content
			if content == "" {
				content = m.TextContent()
			}
			if len(content) > 120 {
				content = content[:120] + "..."
			}
			fmt.Fprintf(&summary, "  Agent: %s\n", strings.ReplaceAll(content, "\n", " "))
		}
	}

	compressed := []domain.TranscriptMessage{
		{Role: "user", Content: summary.String()},
		{Role: "assistant", Content: "Understood. I have the conversation context above."},
	}

	// Append tail
	compressed = append(compressed, msgs[tailStart:]...)
	return compressed
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestCompressTier2 -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/compact.go internal/agent/compact_test.go
git commit -m "feat: add Tier 2 compression — collapse old turns to bullet summaries"
```

---

### Task 4: Improved summary prompt for Tier 3

**Files:**
- Modify: `internal/agent/compact.go`
- Modify: `internal/agent/compact_test.go`

- [ ] **Step 1: Write test for new prompt content**

Add to `compact_test.go`:

```go
func TestCompactSummaryPrompt(t *testing.T) {
	required := []string{
		"Decisions made",
		"Files changed",
		"Current plan",
		"Key constraints",
		"Errors encountered",
		"continue the current task",
	}
	for _, s := range required {
		if !strings.Contains(compactSummaryPrompt, s) {
			t.Errorf("summary prompt missing required section: %q", s)
		}
	}
}
```

- [ ] **Step 2: Replace the summary prompt**

In `compact.go`, replace the existing summarization system prompt and user prompt (in `generateCompactionSummary`) with the new `compactSummaryPrompt` constant:

```go
const compactSummaryPrompt = `You are summarizing a coding conversation. This summary will replace the conversation history. The agent must be able to continue the current task using only this summary and the most recent messages.

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

Be specific. Use exact file paths, function names, and variable names. Maximum 600 words.`
```

Update `generateCompactionSummary` to use this as the system prompt, and pass the serialized messages as the user message (no additional formatting instructions in the user message).

- [ ] **Step 3: Run tests**

Run: `go test ./internal/agent/ -run TestCompactSummaryPrompt -v`
Expected: PASS

- [ ] **Step 4: Run all compact tests**

Run: `go test ./internal/agent/ -run "Compact|Compress|Summarize" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/compact.go internal/agent/compact_test.go
git commit -m "feat: improve compaction summary prompt for better decision retention"
```

---

### Task 5: CheapModel helper and summarization model default

**Files:**
- Modify: `internal/provider/aliases.go`
- Modify: `internal/agent/compact.go`

- [ ] **Step 1: Add CheapModel to aliases.go**

In `internal/provider/aliases.go`:

```go
// CheapModel returns the cheapest model for a given provider,
// suitable for summarization and other background tasks.
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
		return ""
	}
}
```

- [ ] **Step 2: Update summarizationModel in compact.go**

Change `summarizationModel()` to try the cheap model when no explicit `modelCompact` is set:

```go
func (a *Service) summarizationModel() string {
	if a.modelCompact != "" {
		return a.modelCompact
	}
	if a.provider != nil {
		if cheap := provider.CheapModel(a.provider.Name()); cheap != "" {
			return cheap
		}
	}
	return a.modelID
}
```

- [ ] **Step 3: Run build and tests**

Run: `go build ./...`
Run: `go test ./internal/agent/ ./internal/provider/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/provider/aliases.go internal/agent/compact.go
git commit -m "feat: default compaction to cheapest model per provider"
```

---

### Task 6: Wire tiered compression into compactIfNeeded

**Files:**
- Modify: `internal/agent/compact.go`

- [ ] **Step 1: Update thresholds**

Replace the existing constants:

```go
const (
	CompactThreshold    = 100_000
	CompactKeepTail     = 20
)
```

With:

```go
const (
	Tier1Threshold  = 60_000  // tool result compression
	Tier2Threshold  = 75_000  // turn compression
	Tier3Threshold  = 90_000  // LLM summarization
	CompactKeepTail = 20
)
```

- [ ] **Step 2: Rewrite compactIfNeeded**

Replace the existing `compactIfNeeded` method with the tiered version:

```go
func (a *Service) compactIfNeeded(onEvent EventFunc) {
	a.mu.Lock()
	inputTokens := a.lastInputTokens
	a.mu.Unlock()

	if inputTokens == 0 {
		return
	}

	// Tier 1: compress tool results
	if inputTokens > Tier1Threshold && !a.tier1Applied {
		a.mu.Lock()
		tailStart := len(a.messages) - CompactKeepTail
		if tailStart < 2 {
			a.mu.Unlock()
			return
		}
		a.messages = compressTier1(a.messages, tailStart)
		a.tier1Applied = true
		a.mu.Unlock()

		a.logf("agent: tier 1 compression applied (tool results)")
		if onEvent != nil {
			onEvent(Event{Kind: EventCompacted, ModelUsed: "tier1"})
		}
		return
	}

	// Tier 2: compress old turns
	if inputTokens > Tier2Threshold && !a.tier2Applied {
		a.mu.Lock()
		tailStart := len(a.messages) - CompactKeepTail
		if tailStart < 2 {
			a.mu.Unlock()
			return
		}
		a.messages = compressTier2(a.messages, tailStart)
		a.tier2Applied = true
		a.mu.Unlock()

		a.logf("agent: tier 2 compression applied (turn summaries)")
		if onEvent != nil {
			onEvent(Event{Kind: EventCompacted, ModelUsed: "tier2"})
		}
		return
	}

	// Tier 3: LLM summarization (existing flow with new prompt)
	if inputTokens > Tier3Threshold {
		// Reset tier flags since we're doing a full compaction
		a.tier1Applied = false
		a.tier2Applied = false

		// Existing CompactMessages + generateCompactionSummary flow
		a.mu.Lock()
		result := CompactMessages(a.messages)
		a.mu.Unlock()

		if len(result.Dropped) == 0 {
			return
		}

		model := a.summarizationModel()
		a.logf("agent: tier 3 compaction (%d messages dropped, summarizing with %s)", len(result.Dropped), model)
		summary := a.generateCompactionSummary(result.Dropped)
		a.persistCompaction(summary)

		a.mu.Lock()
		// Replace the compacted notice with the summary
		for i, m := range result.Kept {
			if strings.Contains(m.Content, "messages compacted") {
				result.Kept[i].Content = summary
				break
			}
		}
		a.messages = result.Kept
		a.mu.Unlock()

		if onEvent != nil {
			onEvent(Event{Kind: EventCompacted, ModelUsed: model})
		}
	}
}
```

Note: read the existing `compactIfNeeded` carefully before replacing. The above is a template — adapt it to match the exact patterns in the existing code (event emission, locking, logging).

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS — existing compact tests should still work (threshold changed but test mocking should handle it). If any tests reference `CompactThreshold`, update them to use the new tier constants.

- [ ] **Step 4: Run full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/compact.go internal/agent/agent.go
git commit -m "feat: wire tiered compression into compactIfNeeded (60k/75k/90k)"
```

---

### Task 7: Final integration test and cleanup

- [ ] **Step 1: Run go vet**

Run: `go vet ./...`
Expected: Clean

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 3: Build**

Run: `go build -o muxd.exe .`
Expected: Clean

- [ ] **Step 4: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: final cleanup for smarter context compression"
```
