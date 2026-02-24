package agent

import "github.com/batalabs/muxd/internal/domain"

func repairDanglingToolUseMessages(msgs []domain.TranscriptMessage) ([]domain.TranscriptMessage, bool) {
	out := make([]domain.TranscriptMessage, 0, len(msgs))
	changed := false

	for i := 0; i < len(msgs); i++ {
		cur := msgs[i]
		if cur.Role != "assistant" || !cur.HasBlocks() {
			out = append(out, cur)
			continue
		}

		toolUseIDs := collectToolUseIDs(cur.Blocks)
		if len(toolUseIDs) == 0 {
			out = append(out, cur)
			continue
		}

		// Anthropic requires tool_result blocks in the immediate next user message.
		if i+1 >= len(msgs) {
			changed = true
			continue
		}
		next := msgs[i+1]
		if next.Role != "user" || !next.HasBlocks() {
			changed = true
			continue
		}

		resultIDs, hasToolResults := collectToolResultIDs(next.Blocks)
		allMatched := true
		for _, id := range toolUseIDs {
			if !resultIDs[id] {
				allMatched = false
				break
			}
		}
		if !allMatched {
			changed = true
			// Drop the adjacent user tool_result message too, since it's partial.
			if hasToolResults {
				i++
			}
			continue
		}

		out = append(out, cur)
	}

	return out, changed
}

func collectToolUseIDs(blocks []domain.ContentBlock) []string {
	var ids []string
	for _, b := range blocks {
		if b.Type == "tool_use" && b.ToolUseID != "" {
			ids = append(ids, b.ToolUseID)
		}
	}
	return ids
}

func collectToolResultIDs(blocks []domain.ContentBlock) (map[string]bool, bool) {
	ids := map[string]bool{}
	hasToolResults := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			hasToolResults = true
			if b.ToolUseID != "" {
				ids[b.ToolUseID] = true
			}
		}
	}
	return ids, hasToolResults
}
