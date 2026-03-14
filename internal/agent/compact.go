package agent

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

const (
	// Tier1Threshold is the input token count above which tool result
	// compression fires (no LLM call — just shortens verbose tool outputs).
	Tier1Threshold = 60_000
	// Tier2Threshold is the input token count above which old-turn collapse
	// fires (no LLM call — replaces old turns with one-line summaries).
	Tier2Threshold = 75_000
	// Tier3Threshold is the input token count above which full LLM
	// summarization runs (drops middle messages and generates a summary).
	Tier3Threshold = 90_000
	// CompactKeepTail is the number of trailing messages to keep.
	CompactKeepTail = 20
)

// compactSummaryPrompt is the system prompt used when generating a structured
// compaction summary with an LLM. It guides the model to produce a summary
// that captures decisions, files, and the current plan so that the agent can
// continue work using only the summary and the most recent messages.
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

// CompactResult holds the output of a CompactMessages call.
type CompactResult struct {
	Messages   []domain.TranscriptMessage // compacted list (head + placeholder + tail)
	Dropped    []domain.TranscriptMessage // removed middle section
	DidCompact bool
}

// CompactMessages trims the middle of a conversation to fit within token
// limits. It keeps the first user+assistant exchange and the last
// CompactKeepTail messages, inserting a synthetic notice in between.
// The Dropped field contains the removed messages for summarization.
func CompactMessages(msgs []domain.TranscriptMessage) CompactResult {
	if len(msgs) <= CompactKeepTail+2 {
		return CompactResult{Messages: msgs}
	}

	// Keep first user+assistant pair (up to 2 messages).
	headEnd := 0
	for i, m := range msgs {
		if m.Role == "assistant" {
			headEnd = i + 1
			break
		}
	}
	if headEnd == 0 {
		headEnd = 1
	}
	head := msgs[:headEnd]

	// Determine tail start -- ensure it begins on a "user" message so the
	// API sees proper alternation.
	tailStart := len(msgs) - CompactKeepTail
	if tailStart <= headEnd {
		return CompactResult{Messages: msgs}
	}
	for tailStart < len(msgs) && msgs[tailStart].Role != "user" {
		tailStart++
	}
	if tailStart >= len(msgs) {
		return CompactResult{Messages: msgs}
	}
	tail := msgs[tailStart:]

	droppedMsgs := make([]domain.TranscriptMessage, tailStart-headEnd)
	copy(droppedMsgs, msgs[headEnd:tailStart])

	droppedCount := len(droppedMsgs)
	notice := fmt.Sprintf("[%d earlier messages compacted to save context]", droppedCount)

	compacted := make([]domain.TranscriptMessage, 0, len(head)+2+len(tail))
	compacted = append(compacted, head...)
	compacted = append(compacted,
		domain.TranscriptMessage{Role: "user", Content: notice},
		domain.TranscriptMessage{Role: "assistant", Content: "Understood. I'll continue with the context available."},
	)
	compacted = append(compacted, tail...)
	return CompactResult{
		Messages:   compacted,
		Dropped:    droppedMsgs,
		DidCompact: true,
	}
}

// compactIfNeeded applies tiered context compression when input tokens exceed
// configured thresholds. The tiers are applied progressively:
//
//   - Tier 1 (>60k tokens): compress tool results in older messages (no LLM call).
//   - Tier 2 (>75k tokens): collapse old turns to one-line summaries (no LLM call).
//   - Tier 3 (>90k tokens): full LLM summarization — drops middle, generates summary.
//
// Each tier fires at most once before Tier 3 resets the flags for a fresh cycle.
func (a *Service) compactIfNeeded(onEvent EventFunc) {
	a.mu.Lock()
	inputTokens := a.lastInputTokens

	// ── Tier 1: tool result compression ──────────────────────────────────
	if inputTokens > Tier1Threshold && !a.tier1Applied {
		tailStart := len(a.messages) - CompactKeepTail
		if tailStart < 0 {
			tailStart = 0
		}
		a.messages = compressTier1(a.messages, tailStart)
		a.tier1Applied = true
		a.mu.Unlock()
		onEvent(Event{Kind: EventCompacted, ModelUsed: "tier1"})
		return
	}

	// ── Tier 2: old turn collapse ────────────────────────────────────────
	if inputTokens > Tier2Threshold && !a.tier2Applied {
		tailStart := len(a.messages) - CompactKeepTail
		if tailStart < 0 {
			tailStart = 0
		}
		a.messages = compressTier2(a.messages, tailStart)
		a.tier2Applied = true
		a.mu.Unlock()
		onEvent(Event{Kind: EventCompacted, ModelUsed: "tier2"})
		return
	}

	// ── Tier 3: full LLM summarization ───────────────────────────────────
	if inputTokens <= Tier3Threshold {
		a.mu.Unlock()
		return
	}

	// Reset tier flags so tiers 1 & 2 can fire again after this full recompaction.
	a.tier1Applied = false
	a.tier2Applied = false

	result := CompactMessages(a.messages)
	if !result.DidCompact {
		a.mu.Unlock()
		return
	}

	a.messages = result.Messages
	a.lastInputTokens = 0
	droppedCount := len(result.Dropped)
	sumModel := a.summarizationModel()
	a.mu.Unlock()

	onEvent(Event{Kind: EventToolStart, ToolUseID: "internal_compact", ToolName: "compact_context"})

	// Generate LLM summary of dropped messages (unlocked — makes API call).
	summary := a.generateCompactionSummary(result.Dropped)

	// Replace the placeholder user message with the real summary.
	a.mu.Lock()
	for i := range a.messages {
		if strings.Contains(a.messages[i].Content, "compacted to save context") {
			a.messages[i].Content = summary
			break
		}
	}
	a.mu.Unlock()

	a.persistCompaction(summary)
	onEvent(Event{Kind: EventToolDone, ToolUseID: "internal_compact", ToolName: "compact_context", ToolResult: fmt.Sprintf("Compacted %d messages (model: %s)", droppedCount, sumModel)})
	onEvent(Event{Kind: EventCompacted, ModelUsed: sumModel})
}

// persistCompaction saves the current compaction state to the database.
func (a *Service) persistCompaction(summary string) {
	if a.store == nil || a.session == nil {
		return
	}
	maxSeq, err := a.store.MessageMaxSequence(a.session.ID)
	if err != nil || maxSeq == 0 {
		return
	}
	// Use the tail size to compute the cutoff point.
	cutoff := maxSeq - CompactKeepTail
	if cutoff <= 0 {
		return
	}
	if err := a.store.SaveCompaction(a.session.ID, summary, cutoff); err != nil {
		a.logf("agent: save compaction: %v", err)
	}
}

// serializeMessagesForSummary converts dropped messages to a text
// representation suitable for the compaction summary prompt.
func serializeMessagesForSummary(msgs []domain.TranscriptMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.HasBlocks() {
			for _, block := range m.Blocks {
				switch block.Type {
				case "text":
					fmt.Fprintf(&b, "[%s]: %s\n", m.Role, block.Text)
				case "tool_use":
					input := summarizeToolInput(block.ToolInput)
					fmt.Fprintf(&b, "[tool: %s] input: %s\n", block.ToolName, input)
				case "tool_result":
					result := block.ToolResult
					if len(result) > 200 {
						result = result[:200] + "..."
					}
					fmt.Fprintf(&b, "[result: %s] %s\n", block.ToolName, result)
				}
			}
		} else if m.Content != "" {
			fmt.Fprintf(&b, "[%s]: %s\n", m.Role, m.Content)
		}
	}

	text := b.String()
	const maxChars = 30_000
	if len(text) <= maxChars {
		return text
	}

	// Keep first 25% and last 75% when truncating.
	headSize := maxChars / 4
	tailSize := maxChars - headSize
	return text[:headSize] + "\n...[truncated]...\n" + text[len(text)-tailSize:]
}

// summarizeToolInput produces a short string representation of tool input.
func summarizeToolInput(input map[string]any) string {
	if input == nil {
		return "{}"
	}
	var parts []string
	for k, v := range input {
		s := fmt.Sprintf("%v", v)
		if len(s) > 100 {
			s = s[:100] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, s))
	}
	result := "{" + strings.Join(parts, ", ") + "}"
	if len(result) > 300 {
		return result[:300] + "..."
	}
	return result
}

// firstLine returns the first non-empty line of s, trimmed of whitespace.
// Returns s itself (trimmed) if there are no newlines.
func firstLine(s string) string {
	for _, line := range strings.SplitN(s, "\n", -1) {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(s)
}

// countLines returns the number of non-empty lines in s.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// summarizeToolResult produces a short single-line summary of a tool result.
func summarizeToolResult(toolName, result string) string {
	switch toolName {
	case "file_read":
		return fmt.Sprintf("[read: %d lines]", countLines(result))
	case "file_write":
		return fmt.Sprintf("[wrote: %s]", firstLine(result))
	case "file_edit":
		return fmt.Sprintf("[edited: %s]", firstLine(result))
	case "bash":
		out := result
		if len(out) > 80 {
			out = out[:80]
		}
		out = strings.ReplaceAll(out, "\n", " ")
		return fmt.Sprintf("[bash: %s]", out)
	case "grep":
		return fmt.Sprintf("[grep: %d matches]", countLines(result))
	case "glob", "list_files":
		return fmt.Sprintf("[%s: %d files]", toolName, countLines(result))
	case "web_fetch":
		return fmt.Sprintf("[fetched: %d chars]", len(result))
	case "web_search":
		return fmt.Sprintf("[search: %d results]", countLines(result))
	default:
		out := result
		if len(out) > 100 {
			out = out[:100]
		}
		out = strings.ReplaceAll(out, "\n", " ")
		return fmt.Sprintf("[%s: %s]", toolName, out)
	}
}

// compressTier2 collapses messages before tailStart into a single compressed
// summary user message followed by an assistant acknowledgment, then appends
// tail messages untouched. If tailStart is 0 the slice is returned unchanged.
// Each user message in the head becomes "- User: <first 120 chars>".
// Each assistant message becomes "  Agent: <first 120 chars>".
// Content is truncated at 120 chars and newlines are replaced with spaces.
func compressTier2(msgs []domain.TranscriptMessage, tailStart int) []domain.TranscriptMessage {
	if tailStart == 0 {
		out := make([]domain.TranscriptMessage, len(msgs))
		copy(out, msgs)
		return out
	}

	const maxLen = 120
	var b strings.Builder
	b.WriteString("[Compressed conversation history]\n")
	for i := 0; i < tailStart && i < len(msgs); i++ {
		m := msgs[i]
		text := m.TextContent()
		if text == "" {
			text = m.Content
		}
		text = strings.ReplaceAll(text, "\n", " ")
		text = strings.ReplaceAll(text, "\r", " ")
		if len(text) > maxLen {
			text = text[:maxLen]
		}
		if m.Role == "user" {
			fmt.Fprintf(&b, "- User: %s\n", text)
		} else {
			fmt.Fprintf(&b, "  Agent: %s\n", text)
		}
	}

	tail := msgs[tailStart:]
	out := make([]domain.TranscriptMessage, 0, 2+len(tail))
	out = append(out,
		domain.TranscriptMessage{Role: "user", Content: strings.TrimRight(b.String(), "\n")},
		domain.TranscriptMessage{Role: "assistant", Content: "Understood. I have the conversation context above."},
	)
	out = append(out, tail...)
	return out
}

// compressTier1 copies msgs and, for messages before tailStart, replaces any
// tool_result block whose content exceeds 200 chars with a one-line summary
// produced by summarizeToolResult. Messages at or after tailStart are copied
// unchanged. The original slice is never mutated.
func compressTier1(msgs []domain.TranscriptMessage, tailStart int) []domain.TranscriptMessage {
	out := make([]domain.TranscriptMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if i >= tailStart {
			break
		}
		if !out[i].HasBlocks() {
			continue
		}
		// Copy the blocks slice before mutating.
		newBlocks := make([]domain.ContentBlock, len(out[i].Blocks))
		copy(newBlocks, out[i].Blocks)
		for j, block := range newBlocks {
			if block.Type == "tool_result" && len(block.ToolResult) > 200 {
				newBlocks[j].ToolResult = summarizeToolResult(block.ToolName, block.ToolResult)
			}
		}
		out[i].Blocks = newBlocks
	}
	return out
}

// summarizationModel returns the model ID for compaction summaries.
// Priority: explicit modelCompact config > cheapest model for the provider >
// main model.
func (a *Service) summarizationModel() string {
	if a.modelCompact != "" {
		return a.modelCompact
	}
	if a.prov != nil {
		if cheap := provider.CheapModel(a.prov.Name()); cheap != "" {
			return cheap
		}
	}
	return a.modelID
}

// generateCompactionSummary uses a cheap LLM to produce a structured summary
// of dropped messages. Falls back to a simple placeholder on error.
func (a *Service) generateCompactionSummary(dropped []domain.TranscriptMessage) string {
	fallback := fmt.Sprintf("[%d earlier messages were compacted. No summary available.]", len(dropped))

	serialized := serializeMessagesForSummary(dropped)
	if serialized == "" {
		return fallback
	}

	prompt := fmt.Sprintf("Summarize the following conversation excerpt that is being compacted to save context.\n\n---\nConversation to summarize:\n%s", serialized)

	system := compactSummaryPrompt
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: prompt},
	}

	sumModel := a.summarizationModel()
	var respBlocks []domain.ContentBlock
	var err error
	if a.prov == nil {
		return fallback
	}
	respBlocks, _, _, err = a.prov.StreamMessage(
		a.apiKey, sumModel, msgs, nil, system, nil,
	)
	if err != nil {
		return fallback
	}

	var respText string
	for _, b := range respBlocks {
		if b.Type == "text" {
			respText += b.Text
		}
	}
	if strings.TrimSpace(respText) == "" {
		return fallback
	}

	return "[Conversation summary]\n\n" + strings.TrimSpace(respText)
}
