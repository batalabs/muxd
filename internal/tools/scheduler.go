package tools

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// AgentTaskToolName is the sentinel tool name used to schedule full agent tasks.
// Jobs with this tool name spawn a complete agent loop instead of executing a
// single tool call.
const AgentTaskToolName = "__agent_task__"

// ScheduledToolCall represents one scheduled tool execution request.
type ScheduledToolCall struct {
	ID           string
	Source       string // e.g. "tool_job", "legacy_tweet"
	ToolName     string
	ToolInput    map[string]any
	ScheduledFor time.Time
	Recurrence   string
}

// ScheduledToolCallStore provides persistence for scheduled tool calls.
type ScheduledToolCallStore interface {
	DueScheduledToolCalls(now time.Time, limit int) ([]ScheduledToolCall, error)
	MarkScheduledToolCallSucceeded(call ScheduledToolCall, result string, completedAt time.Time) error
	MarkScheduledToolCallFailed(call ScheduledToolCall, errText, result string, attemptedAt time.Time) error
	RescheduleScheduledToolCall(call ScheduledToolCall, next time.Time) error
}

// ScheduledToolCallExecutor executes one scheduled call with provided context.
type ScheduledToolCallExecutor func(call ScheduledToolCall, ctx *ToolContext) (result string, isError bool, err error)

// ToolContextProvider returns the runtime context used by scheduler execution.
type ToolContextProvider func() *ToolContext

// ToolCallScheduler executes scheduled tool calls on an interval.
type ToolCallScheduler struct {
	mu          sync.Mutex
	store       ScheduledToolCallStore
	interval    time.Duration
	ctxProvider ToolContextProvider
	executor    ScheduledToolCallExecutor
	stopCh      chan struct{}
	doneCh      chan struct{}
	running     bool
}

// NewToolCallScheduler creates a generic scheduled tool-call engine.
func NewToolCallScheduler(store ScheduledToolCallStore, interval time.Duration, ctxProvider ToolContextProvider, executor ScheduledToolCallExecutor) *ToolCallScheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	return &ToolCallScheduler{
		store:       store,
		interval:    interval,
		ctxProvider: ctxProvider,
		executor:    executor,
	}
}

// Start begins the background ticker loop.
func (s *ToolCallScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.running = true
	stop := s.stopCh
	done := s.doneCh
	s.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		if err := s.RunOnce(); err != nil {
			fmt.Fprintf(os.Stderr, "scheduler: initial run: %v\n", err)
		}
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := s.RunOnce(); err != nil {
					fmt.Fprintf(os.Stderr, "scheduler: tick run: %v\n", err)
				}
			}
		}
	}()
}

// Stop stops the scheduler loop and waits for shutdown.
func (s *ToolCallScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stop := s.stopCh
	done := s.doneCh
	s.running = false
	s.mu.Unlock()
	close(stop)
	<-done
}

// RunOnce processes due scheduled tool calls once.
func (s *ToolCallScheduler) RunOnce() error {
	if s.store == nil || s.executor == nil {
		return nil
	}
	calls, err := s.store.DueScheduledToolCalls(nowFunc().UTC(), 25)
	if err != nil {
		return err
	}
	for _, call := range calls {
		attempted := nowFunc().UTC()
		ctx := &ToolContext{}
		if s.ctxProvider != nil {
			if provided := s.ctxProvider(); provided != nil {
				ctx = provided
			}
		}

		if !isSchedulerAllowed(call.ToolName, ctx) {
			if err := s.store.MarkScheduledToolCallFailed(call, "scheduled tool is not allowed by policy", "", attempted); err != nil {
				fmt.Fprintf(os.Stderr, "scheduler: mark failed (policy): %v\n", err)
			}
			continue
		}

		result, isToolError, execErr := s.executor(call, ctx)
		if execErr != nil {
			if err := s.store.MarkScheduledToolCallFailed(call, execErr.Error(), result, attempted); err != nil {
				fmt.Fprintf(os.Stderr, "scheduler: mark failed (exec): %v\n", err)
			}
			continue
		}
		if isToolError {
			if err := s.store.MarkScheduledToolCallFailed(call, "tool execution returned an error result", result, attempted); err != nil {
				fmt.Fprintf(os.Stderr, "scheduler: mark failed (tool error): %v\n", err)
			}
			continue
		}
		if err := s.store.MarkScheduledToolCallSucceeded(call, result, attempted); err != nil {
			fmt.Fprintf(os.Stderr, "scheduler: mark succeeded: %v\n", err)
		}
		next, recurring := nextRecurringTime(call.Recurrence, call.ScheduledFor)
		if recurring {
			if err := s.store.RescheduleScheduledToolCall(call, next); err != nil {
				fmt.Fprintf(os.Stderr, "scheduler: reschedule: %v\n", err)
			}
		}
	}
	return nil
}

func isSchedulerAllowed(toolName string, ctx *ToolContext) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false
	}
	// Agent tasks bypass the per-tool allowlist — the spawned agent
	// enforces its own tool policy.
	if name == AgentTaskToolName {
		return true
	}
	if ctx != nil && ctx.Disabled != nil && ctx.Disabled[name] {
		return false
	}
	if ctx == nil || len(ctx.ScheduledAllowed) == 0 {
		return false
	}
	return ctx.ScheduledAllowed[name]
}

func nextRecurringTime(recurrence string, from time.Time) (time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(recurrence)) {
	case "daily":
		return from.UTC().Add(24 * time.Hour), true
	case "hourly":
		return from.UTC().Add(time.Hour), true
	default:
		return time.Time{}, false
	}
}
