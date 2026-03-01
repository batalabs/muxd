package tools

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/provider"
)

func logReadTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "log_read",
			Description: "Read recent muxd daemon log entries. Returns the last N lines from the log file. Useful for debugging errors, checking scheduler activity, and reviewing system behavior. Only call once per conversation turn — do not repeat if you already have the result.",
			Properties: map[string]provider.ToolProp{
				"lines": {Type: "string", Description: "Number of lines to return from the end of the log (default: 50, max: 500)"},
			},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			n := 50
			if v, ok := input["lines"].(string); ok && v != "" {
				if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
					n = parsed
				}
			}
			if n > 500 {
				n = 500
			}

			logPath := config.LogPath()
			if logPath == "" {
				return "", fmt.Errorf("log file path not available")
			}

			data, err := os.ReadFile(logPath)
			if err != nil {
				if os.IsNotExist(err) {
					return "Log file is empty (no entries yet).", nil
				}
				return "", fmt.Errorf("reading log: %w", err)
			}

			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if len(lines) > n {
				lines = lines[len(lines)-n:]
			}

			if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
				return "Log file is empty (no entries yet).", nil
			}

			return fmt.Sprintf("Last %d log lines:\n\n%s", len(lines), strings.Join(lines, "\n")), nil
		},
	}
}
