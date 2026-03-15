package tui

import (
	"fmt"
	"strings"
	"time"
)

// thinkingMessages are fun status messages shown while the agent is thinking.
var thinkingMessages = []string{
	"Thinking...",
	"Pondering...",
	"Cooking something up...",
	"Connecting the dots...",
	"Brewing ideas...",
	"Mulling it over...",
	"Working on it...",
	"Crunching the problem...",
	"Processing...",
	"Almost there...",
}

// describeToolStart returns a short description of what a tool is about to do.
func describeToolStart(toolName string, input map[string]any) string {
	getStr := func(key string) string {
		if v, ok := input[key].(string); ok {
			return v
		}
		return ""
	}
	shorten := func(s string, max int) string {
		if len(s) > max {
			return s[:max]
		}
		return s
	}

	switch toolName {
	case "file_read":
		if p := getStr("path"); p != "" {
			return "Reading " + shorten(p, 40)
		}
	case "file_write":
		if p := getStr("path"); p != "" {
			return "Writing " + shorten(p, 40)
		}
	case "file_edit":
		if p := getStr("path"); p != "" {
			return "Editing " + shorten(p, 40)
		}
	case "bash":
		if cmd := getStr("command"); cmd != "" {
			return "Running: " + shorten(cmd, 50)
		}
	case "grep":
		if pat := getStr("pattern"); pat != "" {
			return "Searching for " + shorten(pat, 30)
		}
	case "glob":
		if pat := getStr("pattern"); pat != "" {
			return "Finding " + shorten(pat, 30)
		}
	case "web_search":
		if q := getStr("query"); q != "" {
			return "Searching: " + shorten(q, 40)
		}
	case "web_fetch":
		if u := getStr("url"); u != "" {
			return "Fetching " + shorten(u, 40)
		}
	case "task":
		if d := getStr("description"); d != "" {
			return "Sub-agent: " + shorten(d, 30)
		}
	case "consult":
		return "Consulting another model"
	case "tool_create":
		if n := getStr("name"); n != "" {
			return "Creating tool: " + n
		}
	case "memory_write":
		if k := getStr("key"); k != "" {
			return "Saving to memory: " + k
		}
	case "memory_read":
		return "Reading memory"
	}
	return "Running " + toolName
}

// describeToolAction returns a short human-readable summary of what a tool did.
func describeToolAction(toolName, result string) string {
	// Extract a path-like token from the result for file tools.
	extractPath := func(s string) string {
		for _, word := range strings.Fields(s) {
			// Look for something with an extension.
			if strings.Contains(word, ".") && len(word) > 2 && !strings.HasPrefix(word, "(") {
				word = strings.TrimRight(word, ",:;)")
				if len(word) > 40 {
					// Show just the filename.
					if i := strings.LastIndexAny(word, "/\\"); i >= 0 {
						return word[i+1:]
					}
					return word[:40]
				}
				return word
			}
		}
		return ""
	}

	switch toolName {
	case "file_read":
		if p := extractPath(result); p != "" {
			return "Read " + p
		}
		return "Read file"
	case "file_write":
		if p := extractPath(result); p != "" {
			return "Wrote " + p
		}
		return "Wrote file"
	case "file_edit":
		if p := extractPath(result); p != "" {
			return "Edited " + p
		}
		return "Edited file"
	case "bash":
		return "Ran command"
	case "grep":
		lines := strings.Split(strings.TrimSpace(result), "\n")
		return fmt.Sprintf("Searched (%d matches)", len(lines))
	case "glob", "list_files":
		lines := strings.Split(strings.TrimSpace(result), "\n")
		return fmt.Sprintf("Found %d files", len(lines))
	case "web_search":
		return "Searched the web"
	case "web_fetch":
		return "Fetched URL"
	case "memory_write":
		return "Updated memory"
	case "memory_read":
		return "Read memory"
	case "consult":
		return "Got second opinion"
	case "tool_create":
		return "Created custom tool"
	case "task":
		return "Ran sub-agent"
	default:
		return "Ran " + toolName
	}
}

// buildActivityStatus returns a rich status string for the spinner area.
func (m Model) buildActivityStatus() string {
	var parts []string

	// Primary status: streaming, current tool, or fun thinking message.
	if m.streaming {
		if m.turnLastAction != "" {
			parts = append(parts, m.turnLastAction+" → Writing response")
		} else {
			parts = append(parts, "Writing response...")
		}
	} else if m.turnCurrentTool != "" {
		parts = append(parts, "Running "+m.turnCurrentTool)
	} else if m.toolStatus != "" && !strings.HasPrefix(m.toolStatus, "Thinking") {
		parts = append(parts, m.toolStatus)
	} else {
		// Pick a fun message based on elapsed time (rotates every 4 seconds).
		elapsed := time.Since(m.turnStartTime)
		idx := int(elapsed.Seconds()/4) % len(thinkingMessages)
		parts = append(parts, thinkingMessages[idx])
	}

	// Stats: tool count.
	if m.turnToolCount > 0 {
		if m.turnToolCount == 1 {
			parts = append(parts, "1 tool call")
		} else {
			parts = append(parts, fmt.Sprintf("%d tool calls", m.turnToolCount))
		}
	}

	// Stats: files changed.
	if len(m.turnFilesChanged) > 0 {
		if len(m.turnFilesChanged) == 1 {
			parts = append(parts, "1 file changed")
		} else {
			parts = append(parts, fmt.Sprintf("%d files changed", len(m.turnFilesChanged)))
		}
	}

	// Stats: elapsed time (only show after 5 seconds).
	elapsed := time.Since(m.turnStartTime)
	if elapsed >= 5*time.Second {
		parts = append(parts, fmt.Sprintf("%ds", int(elapsed.Seconds())))
	}

	return strings.Join(parts, " · ")
}
