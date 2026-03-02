package tools

import (
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// isSchedulerAllowed
// ---------------------------------------------------------------------------

func TestIsSchedulerAllowed(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		ctx      *ToolContext
		want     bool
	}{
		{"empty tool name", "", nil, false},
		{"nil context", "sms_send", nil, false},
		{"empty allowed set", "sms_send", &ToolContext{ScheduledAllowed: map[string]bool{}}, false},
		{"tool allowed", "sms_send", &ToolContext{ScheduledAllowed: map[string]bool{"sms_send": true}}, true},
		{"tool not in allowed set", "bash", &ToolContext{ScheduledAllowed: map[string]bool{"sms_send": true}}, false},
		{"tool disabled", "sms_send", &ToolContext{
			ScheduledAllowed: map[string]bool{"sms_send": true},
			Disabled:         map[string]bool{"sms_send": true},
		}, false},
		{"case insensitive", "SMS_SEND", &ToolContext{ScheduledAllowed: map[string]bool{"sms_send": true}}, true},
		{"agent task bypasses allowlist", AgentTaskToolName, nil, true},
		{"agent task bypasses allowlist with empty context", AgentTaskToolName, &ToolContext{}, true},
		{"agent task bypasses even with empty allowed set", AgentTaskToolName, &ToolContext{ScheduledAllowed: map[string]bool{}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSchedulerAllowed(tt.toolName, tt.ctx)
			if got != tt.want {
				t.Errorf("isSchedulerAllowed(%q, ...) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// nextRecurringTime
// ---------------------------------------------------------------------------

func TestNextRecurringTime(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)

	t.Run("daily adds 24 hours", func(t *testing.T) {
		next, ok := nextRecurringTime("daily", base)
		if !ok {
			t.Fatal("expected recurring=true")
		}
		want := base.Add(24 * time.Hour)
		if !next.Equal(want) {
			t.Errorf("next = %v, want %v", next, want)
		}
	})

	t.Run("hourly adds 1 hour", func(t *testing.T) {
		next, ok := nextRecurringTime("hourly", base)
		if !ok {
			t.Fatal("expected recurring=true")
		}
		want := base.Add(time.Hour)
		if !next.Equal(want) {
			t.Errorf("next = %v, want %v", next, want)
		}
	})

	t.Run("once returns false", func(t *testing.T) {
		_, ok := nextRecurringTime("once", base)
		if ok {
			t.Error("expected recurring=false for once")
		}
	})

	t.Run("empty returns false", func(t *testing.T) {
		_, ok := nextRecurringTime("", base)
		if ok {
			t.Error("expected recurring=false for empty")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		_, ok := nextRecurringTime("  DAILY  ", base)
		if !ok {
			t.Error("expected recurring=true for DAILY")
		}
	})
}

// ---------------------------------------------------------------------------
// NewToolCallScheduler + Start/Stop
// ---------------------------------------------------------------------------

func TestToolCallScheduler_StartStop(t *testing.T) {
	st := &fakeSchedulerStore{}
	s := NewToolCallScheduler(st, 50*time.Millisecond, nil, func(call ScheduledToolCall, ctx *ToolContext) (string, bool, error) {
		return "ok", false, nil
	})
	s.Start()
	// Double-start should be a no-op.
	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop()
	// Double-stop should be a no-op.
	s.Stop()
}

func TestNewToolCallScheduler_defaultInterval(t *testing.T) {
	s := NewToolCallScheduler(nil, 0, nil, nil)
	if s.interval != time.Minute {
		t.Errorf("expected default interval of 1 minute, got %v", s.interval)
	}
}

// ---------------------------------------------------------------------------
// fakeSchedulerStore
// ---------------------------------------------------------------------------

type fakeSchedulerStore struct {
	dueJobs        []ScheduledToolCall
	succeededIDs   []string
	failedIDs      []string
	rescheduledIDs []string
}

func (f *fakeSchedulerStore) DueScheduledToolCalls(now time.Time, limit int) ([]ScheduledToolCall, error) {
	return append([]ScheduledToolCall(nil), f.dueJobs...), nil
}

func (f *fakeSchedulerStore) MarkScheduledToolCallSucceeded(call ScheduledToolCall, result string, completedAt time.Time) error {
	f.succeededIDs = append(f.succeededIDs, call.ID)
	return nil
}

func (f *fakeSchedulerStore) MarkScheduledToolCallFailed(call ScheduledToolCall, errText, result string, attemptedAt time.Time) error {
	f.failedIDs = append(f.failedIDs, call.ID)
	return nil
}

func (f *fakeSchedulerStore) RescheduleScheduledToolCall(call ScheduledToolCall, next time.Time) error {
	f.rescheduledIDs = append(f.rescheduledIDs, call.ID)
	return nil
}

func TestToolCallScheduler_RunOnce(t *testing.T) {
	t.Run("runs allowed job successfully", func(t *testing.T) {
		st := &fakeSchedulerStore{
			dueJobs: []ScheduledToolCall{
				{ID: "a", ToolName: "sms_send", ToolInput: map[string]any{"text": "hello"}, ScheduledFor: time.Now().Add(-time.Minute), Recurrence: "once"},
			},
		}
		s := NewToolCallScheduler(st, time.Minute, func() *ToolContext {
			return &ToolContext{
				ScheduledAllowed: map[string]bool{"sms_send": true},
				Disabled:         map[string]bool{},
			}
		}, func(call ScheduledToolCall, ctx *ToolContext) (string, bool, error) {
			return "ok", false, nil
		})
		if err := s.RunOnce(); err != nil {
			t.Fatalf("RunOnce error: %v", err)
		}
		if len(st.succeededIDs) != 1 || st.succeededIDs[0] != "a" {
			t.Fatalf("succeededIDs = %v", st.succeededIDs)
		}
	})

	t.Run("blocks disallowed tool", func(t *testing.T) {
		st := &fakeSchedulerStore{
			dueJobs: []ScheduledToolCall{
				{ID: "b", ToolName: "bash", ToolInput: map[string]any{"command": "echo hi"}, ScheduledFor: time.Now().Add(-time.Minute), Recurrence: "once"},
			},
		}
		s := NewToolCallScheduler(st, time.Minute, func() *ToolContext {
			return &ToolContext{
				ScheduledAllowed: map[string]bool{"sms_send": true},
			}
		}, func(call ScheduledToolCall, ctx *ToolContext) (string, bool, error) {
			return "should-not-run", false, nil
		})
		if err := s.RunOnce(); err != nil {
			t.Fatalf("RunOnce error: %v", err)
		}
		if len(st.failedIDs) != 1 || st.failedIDs[0] != "b" {
			t.Fatalf("failedIDs = %v", st.failedIDs)
		}
	})

	t.Run("marks failure on executor error", func(t *testing.T) {
		st := &fakeSchedulerStore{
			dueJobs: []ScheduledToolCall{
				{ID: "c", ToolName: "sms_send", ToolInput: map[string]any{"text": "hello"}, ScheduledFor: time.Now().Add(-time.Minute), Recurrence: "once"},
			},
		}
		s := NewToolCallScheduler(st, time.Minute, func() *ToolContext {
			return &ToolContext{ScheduledAllowed: map[string]bool{"sms_send": true}}
		}, func(call ScheduledToolCall, ctx *ToolContext) (string, bool, error) {
			return "", false, errors.New("boom")
		})
		if err := s.RunOnce(); err != nil {
			t.Fatalf("RunOnce error: %v", err)
		}
		if len(st.failedIDs) != 1 || st.failedIDs[0] != "c" {
			t.Fatalf("failedIDs = %v", st.failedIDs)
		}
	})

	t.Run("reschedules recurring on success", func(t *testing.T) {
		st := &fakeSchedulerStore{
			dueJobs: []ScheduledToolCall{
				{ID: "d", ToolName: "sms_send", ToolInput: map[string]any{"text": "hello"}, ScheduledFor: time.Now().Add(-time.Minute), Recurrence: "daily"},
			},
		}
		s := NewToolCallScheduler(st, time.Minute, func() *ToolContext {
			return &ToolContext{ScheduledAllowed: map[string]bool{"sms_send": true}}
		}, func(call ScheduledToolCall, ctx *ToolContext) (string, bool, error) {
			return "ok", false, nil
		})
		if err := s.RunOnce(); err != nil {
			t.Fatalf("RunOnce error: %v", err)
		}
		if len(st.rescheduledIDs) != 1 || st.rescheduledIDs[0] != "d" {
			t.Fatalf("rescheduledIDs = %v", st.rescheduledIDs)
		}
	})
}
