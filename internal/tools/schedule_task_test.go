package tools

import (
	"strings"
	"testing"
	"time"
)

func TestScheduleTaskTool(t *testing.T) {
	tool := scheduleTaskTool()

	fakeNow := time.Date(2026, 2, 24, 12, 0, 0, 0, time.UTC)
	origNow := nowFunc
	t.Cleanup(func() { nowFunc = origNow })
	nowFunc = func() time.Time { return fakeNow }

	makeCtx := func() *ToolContext {
		return &ToolContext{
			ScheduleTool: func(toolName string, input map[string]any, scheduledFor time.Time, recurrence string) (string, error) {
				return "job-1234", nil
			},
		}
	}

	tests := []struct {
		name    string
		input   map[string]any
		ctx     *ToolContext
		wantErr string
		wantOK  string
	}{
		{
			name:   "valid HH:MM input",
			input:  map[string]any{"prompt": "search tweets about Go", "time": "16:00"},
			ctx:    makeCtx(),
			wantOK: "Scheduled agent task job-1234",
		},
		{
			name:   "valid RFC3339 input",
			input:  map[string]any{"prompt": "post summary", "time": "2026-02-25T10:00:00Z"},
			ctx:    makeCtx(),
			wantOK: "Scheduled agent task job-1234",
		},
		{
			name:   "valid with daily recurrence",
			input:  map[string]any{"prompt": "daily digest", "time": "08:00", "recurrence": "daily"},
			ctx:    makeCtx(),
			wantOK: "daily",
		},
		{
			name:   "valid with hourly recurrence",
			input:  map[string]any{"prompt": "check mentions", "time": "14:00", "recurrence": "hourly"},
			ctx:    makeCtx(),
			wantOK: "hourly",
		},
		{
			name:    "empty prompt",
			input:   map[string]any{"prompt": "", "time": "16:00"},
			ctx:     makeCtx(),
			wantErr: "prompt is required",
		},
		{
			name:    "missing prompt",
			input:   map[string]any{"time": "16:00"},
			ctx:     makeCtx(),
			wantErr: "prompt is required",
		},
		{
			name:    "empty time",
			input:   map[string]any{"prompt": "do something", "time": ""},
			ctx:     makeCtx(),
			wantErr: "time is required",
		},
		{
			name:    "missing time",
			input:   map[string]any{"prompt": "do something"},
			ctx:     makeCtx(),
			wantErr: "time is required",
		},
		{
			name:    "invalid time format",
			input:   map[string]any{"prompt": "do something", "time": "not-a-time"},
			ctx:     makeCtx(),
			wantErr: "invalid time",
		},
		{
			name:    "invalid recurrence",
			input:   map[string]any{"prompt": "do something", "time": "16:00", "recurrence": "weekly"},
			ctx:     makeCtx(),
			wantErr: "invalid recurrence",
		},
		{
			name:    "nil context",
			input:   map[string]any{"prompt": "do something", "time": "16:00"},
			ctx:     nil,
			wantErr: "scheduler not available",
		},
		{
			name:    "nil ScheduleTool",
			input:   map[string]any{"prompt": "do something", "time": "16:00"},
			ctx:     &ToolContext{},
			wantErr: "scheduler not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(tt.input, tt.ctx)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, tt.wantOK) {
				t.Errorf("expected result containing %q, got: %s", tt.wantOK, result)
			}
		})
	}
}
