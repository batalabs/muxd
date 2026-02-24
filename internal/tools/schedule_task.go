package tools

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// schedule_task — schedule a full agent loop for future execution
// ---------------------------------------------------------------------------

func scheduleTaskTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "schedule_task",
			Description: "Schedule a multi-step agent task for future execution. At the scheduled time, a full agent loop is spawned with the given prompt and all tools. Use this for complex workflows that require multiple tool calls (e.g., 'search for tweets about X and reply to 5').",
			Properties: map[string]provider.ToolProp{
				"prompt":     {Type: "string", Description: "The prompt describing the task to execute"},
				"time":       {Type: "string", Description: "When to execute (RFC3339 e.g. '2026-02-24T16:00:00Z' or HH:MM e.g. '16:00')"},
				"recurrence": {Type: "string", Description: "How often to repeat: 'once' (default), 'daily', or 'hourly'"},
			},
			Required: []string{"prompt", "time"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			prompt, _ := input["prompt"].(string)
			if strings.TrimSpace(prompt) == "" {
				return "", fmt.Errorf("prompt is required")
			}

			rawTime, _ := input["time"].(string)
			if strings.TrimSpace(rawTime) == "" {
				return "", fmt.Errorf("time is required")
			}

			scheduledFor, err := ParseTweetScheduleTime(rawTime, nowFunc())
			if err != nil {
				return "", fmt.Errorf("invalid time: %w", err)
			}

			recurrence := "once"
			if v, _ := input["recurrence"].(string); v != "" {
				v = strings.ToLower(strings.TrimSpace(v))
				switch v {
				case "once", "daily", "hourly":
					recurrence = v
				default:
					return "", fmt.Errorf("invalid recurrence %q: must be once, daily, or hourly", v)
				}
			}

			if ctx == nil || ctx.ScheduleTool == nil {
				return "", fmt.Errorf("scheduler not available")
			}

			toolInput := map[string]any{"prompt": prompt}
			id, err := ctx.ScheduleTool(AgentTaskToolName, toolInput, scheduledFor, recurrence)
			if err != nil {
				return "", fmt.Errorf("scheduling task: %w", err)
			}

			return fmt.Sprintf("Scheduled agent task %s for %s (%s):\n%s",
				id, scheduledFor.Local().Format("2006-01-02 15:04"), recurrence, prompt), nil
		},
	}
}
