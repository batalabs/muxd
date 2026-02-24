package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

const (
	// CompactThreshold is the input token count above which compaction runs.
	// Set at 100k to compact early for tool-heavy workflows.
	CompactThreshold = 100_000
	// CompactKeepTail is the number of trailing messages to keep.
	CompactKeepTail = 20
)

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

// compactIfNeeded checks if context exceeds the threshold and performs
// compaction with LLM-generated summary if needed.
// Acts as a safety net — if server-side compaction (Anthropic) triggers first,
// input tokens stay below threshold and this never activates.
func (a *Service) compactIfNeeded(onEvent EventFunc) {
	a.mu.Lock()
	if a.lastInputTokens <= CompactThreshold {
		a.mu.Unlock()
		return
	}

	result := CompactMessages(a.messages)
	if !result.DidCompact {
		a.mu.Unlock()
		return
	}

	a.messages = result.Messages
	a.lastInputTokens = 0
	a.mu.Unlock()

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
	onEvent(Event{Kind: EventCompacted})
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
		fmt.Fprintf(os.Stderr, "agent: save compaction: %v\n", err)
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

// summarizationModel returns a cheap, fast model ID for compaction summaries.
// Uses Haiku for Anthropic, gpt-4o-mini for OpenAI, or falls back to the
// current model if the provider is unknown.
func (a *Service) summarizationModel() string {
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

// generateCompactionSummary uses a cheap LLM to produce a structured summary
// of dropped messages. Falls back to a simple placeholder on error.
func (a *Service) generateCompactionSummary(dropped []domain.TranscriptMessage) string {
	fallback := fmt.Sprintf("[%d earlier messages were compacted. No summary available.]", len(dropped))

	serialized := serializeMessagesForSummary(dropped)
	if serialized == "" {
		return fallback
	}

	prompt := fmt.Sprintf(`Summarize the following conversation excerpt that is being compacted to save context. Produce a concise structured summary that preserves key information for continuing the conversation.

Format your response as:
## Topics discussed
- (bullet points)

## Files modified
- (list file paths, or "none" if no files were changed)

## Tools used
- (list tool names and what they did)

## Key decisions
- (important choices or conclusions)

## Current task state
(brief description of where things stand)

---
Conversation to summarize:
%s`, serialized)

	system := "You are a conversation summarizer. Produce a concise structured summary. Maximum 500 words."
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: prompt},
	}

	sumModel := a.summarizationModel()
	var respBlocks []domain.ContentBlock
	var err error
	if a.prov != nil {
		respBlocks, _, _, err = a.prov.StreamMessage(
			a.apiKey, sumModel, msgs, nil, system, nil,
		)
	} else {
		respBlocks, _, _, err = provider.StreamMessagePure(
			a.apiKey, sumModel, msgs, nil, system, nil,
		)
	}
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
