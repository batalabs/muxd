package agent

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/checkpoint"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/tools"
)

// Submit sends a user message and runs the full agent loop synchronously.
// The caller should wrap this in a goroutine. Events are delivered via onEvent.
// Submit blocks until the turn is complete or cancelled.
func (a *Service) Submit(userText string, onEvent EventFunc) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		onEvent(Event{Kind: EventError, Err: fmt.Errorf("agent is already running")})
		return
	}
	a.running = true
	a.cancelled = false
	a.agentLoopCount = 0
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	// 1. Append user message and persist
	userMsg := domain.TranscriptMessage{Role: "user", Content: userText}
	a.mu.Lock()
	a.messages = append(a.messages, userMsg)
	a.mu.Unlock()

	if a.store != nil && a.session != nil {
		if err := a.store.AppendMessage(a.session.ID, "user", userText, 0); err != nil {
			fmt.Fprintf(os.Stderr, "agent: persist user message: %v\n", err)
		}
	}

	// 2. Compact if context too large
	a.compactIfNeeded(onEvent)

	// Resolve CWD for system prompt.
	// Getwd error is intentionally ignored — empty string is a valid fallback.
	cwd := a.Cwd
	if cwd == "" {
		cwd, _ = tools.Getwd() //nolint:errcheck // fallback to empty string
	}

	// 3. Agent loop
	for {
		// Build ToolContext each iteration so hot-reloaded config
		// (e.g. /x auth completing mid-loop) is picked up.
		a.mu.Lock()
		if a.cancelled {
			a.mu.Unlock()
			return
		}
		a.agentLoopCount++
		loopCount := a.agentLoopCount
		messages := make([]domain.TranscriptMessage, len(a.messages))
		copy(messages, a.messages)

		disabled := make(map[string]bool, len(a.disabledTools))
		for k, v := range a.disabledTools {
			disabled[k] = v
		}
		var mcpMgr *mcp.Manager
		if a.mcpManager != nil {
			mcpMgr = a.mcpManager
		}
		toolCtx := &tools.ToolContext{
			Cwd:              cwd,
			Todos:            &a.todos,
			Memory:           a.memory,
			PlanMode:         &a.planMode,
			Disabled:         disabled,
			BraveAPIKey:      a.braveAPIKey,
			XClientID:        a.xClientID,
			XClientSecret:    a.xClientSecret,
			XAccessToken:     a.xAccessToken,
			XRefreshToken:    a.xRefreshToken,
			XTokenExpiry:     a.xTokenExpiry,
			SaveXOAuthTokens: a.xTokenSaver,
			MCP:              mcpMgr,
		}
		if schedStore, ok := a.store.(ScheduledToolJobStore); ok {
			toolCtx.ScheduleTool = schedStore.CreateScheduledToolJob
			toolCtx.ListScheduledJobs = func(toolName string, limit int) ([]tools.ScheduledJobInfo, error) {
				jobs, err := schedStore.ListScheduledToolJobs(limit)
				if err != nil {
					return nil, err
				}
				var out []tools.ScheduledJobInfo
				for _, j := range jobs {
					if j.ToolName != toolName {
						continue
					}
					out = append(out, tools.ScheduledJobInfo{
						ID:           j.ID,
						ToolName:     j.ToolName,
						ToolInput:    j.ToolInput,
						ScheduledFor: j.ScheduledFor,
						Recurrence:   j.Recurrence,
						Status:       j.Status,
						CreatedAt:    j.CreatedAt,
					})
				}
				return out, nil
			}
			toolCtx.CancelScheduledJob = schedStore.CancelScheduledToolJob
			toolCtx.UpdateScheduledJob = schedStore.UpdateScheduledToolJob
		}
		if !a.isSubAgent {
			toolCtx.SpawnAgent = a.SpawnSubAgent
		}
		if repaired, changed := repairDanglingToolUseMessages(messages); changed {
			a.messages = make([]domain.TranscriptMessage, len(repaired))
			copy(a.messages, repaired)
			messages = repaired
		}
		a.mu.Unlock()

		if loopCount > LoopLimit {
			onEvent(Event{
				Kind: EventError,
				Err:  fmt.Errorf("agent loop limit exceeded (%d iterations)", LoopLimit),
			})
			return
		}

		// 3a. Stream API call
		var blocks []domain.ContentBlock
		var stopReason string
		var usage provider.Usage
		var err error

		var toolSpecs []provider.ToolSpec
		if a.isSubAgent {
			toolSpecs = tools.AllToolSpecsForSubAgent()
		} else {
			toolSpecs = tools.AllToolSpecsForModeWithDisabled(a.planMode, disabled)
		}
		// Append MCP tool specs (filtered by disabled set).
		var mcpToolNames []string
		if mcpMgr != nil {
			for _, spec := range mcpMgr.ToolSpecs() {
				if !disabled[spec.Name] {
					toolSpecs = append(toolSpecs, spec)
					mcpToolNames = append(mcpToolNames, spec.Name)
				}
			}
		}
		memoryText := ""
		if a.memory != nil {
			memoryText = a.memory.FormatForPrompt()
		}
		system := provider.BuildSystemPrompt(cwd, mcpToolNames, memoryText)

		blocks, stopReason, usage, err = a.callProviderWithRetry(
			messages, toolSpecs, system,
			func(delta string) {
				onEvent(Event{Kind: EventDelta, DeltaText: delta})
			},
			onEvent,
		)
		if err != nil {
			onEvent(Event{Kind: EventError, Err: err})
			// Persist the error as an assistant message
			a.mu.Lock()
			errMsg := domain.TranscriptMessage{Role: "assistant", Content: "Error: " + err.Error()}
			a.messages = append(a.messages, errMsg)
			a.mu.Unlock()
			if a.store != nil && a.session != nil {
				if err := a.store.AppendMessage(a.session.ID, "assistant", errMsg.Content, 0); err != nil {
					fmt.Fprintf(os.Stderr, "agent: persist error message: %v\n", err)
				}
			}
			return
		}

		// 3b. Update token counts and build assistant message
		a.mu.Lock()
		a.inputTokens += usage.InputTokens
		a.outputTokens += usage.OutputTokens
		a.lastInputTokens = usage.InputTokens

		var asstMsg domain.TranscriptMessage
		if len(blocks) > 0 {
			asstMsg = domain.TranscriptMessage{Role: "assistant", Blocks: blocks}
			asstMsg.Content = asstMsg.TextContent()
		} else {
			asstMsg = domain.TranscriptMessage{Role: "assistant", Content: "I could not generate a text response."}
		}
		a.messages = append(a.messages, asstMsg)
		a.mu.Unlock()

		// Persist assistant message
		if a.store != nil && a.session != nil {
			if len(blocks) > 0 {
				if err := a.store.AppendMessageBlocks(a.session.ID, "assistant", blocks, usage.OutputTokens); err != nil {
					fmt.Fprintf(os.Stderr, "agent: persist assistant blocks: %v\n", err)
				}
			} else {
				if err := a.store.AppendMessage(a.session.ID, "assistant", asstMsg.Content, usage.OutputTokens); err != nil {
					fmt.Fprintf(os.Stderr, "agent: persist assistant message: %v\n", err)
				}
			}
			if err := a.store.UpdateSessionTokens(a.session.ID, a.inputTokens, a.outputTokens); err != nil {
				fmt.Fprintf(os.Stderr, "agent: update session tokens: %v\n", err)
			}
		}

		// Auto-title on first response
		a.mu.Lock()
		shouldTitle := !a.titled && a.store != nil && a.session != nil
		if shouldTitle {
			a.titled = true
		}
		a.mu.Unlock()

		if shouldTitle {
			a.generateAndSetTitle(asstMsg.TextContent(), onEvent)
		}

		// Detect server-side compaction blocks in response
		for _, b := range blocks {
			if b.Type == "compaction" {
				onEvent(Event{Kind: EventCompacted})
				break
			}
		}

		onEvent(Event{
			Kind:                     EventStreamDone,
			Blocks:                   blocks,
			StopReason:               stopReason,
			InputTokens:              usage.InputTokens,
			OutputTokens:             usage.OutputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
		})

		// 3c. If not tool_use, the turn is done
		if stopReason != "tool_use" {
			onEvent(Event{Kind: EventTurnDone, StopReason: stopReason})
			return
		}

		// 3d. Create checkpoint if git available
		a.mu.Lock()
		gitAvail := a.gitAvailable
		a.mu.Unlock()

		if gitAvail && a.session != nil {
			sha, cpErr := checkpoint.GitStashCreate()
			if cpErr == nil {
				cp := checkpoint.Checkpoint{TurnNumber: loopCount}
				if sha == "" {
					cp.IsClean = true
				} else {
					cp.SHA = sha
					ref := fmt.Sprintf("refs/muxd/%s/%d", a.session.ID[:8], loopCount)
					if err := checkpoint.GitUpdateRef(ref, sha); err != nil {
						fmt.Fprintf(os.Stderr, "agent: git update-ref: %v\n", err)
					}
				}
				a.mu.Lock()
				a.checkpoints = append(a.checkpoints, cp)
				a.redoStack = nil
				a.mu.Unlock()
			}
		}

		// 3e. Collect tool_use blocks
		var toolUseBlocks []domain.ContentBlock
		for _, b := range blocks {
			if b.Type == "tool_use" {
				toolUseBlocks = append(toolUseBlocks, b)
			}
		}

		// Check if any tool requires sequential execution.
		hasSequential := false
		for _, b := range toolUseBlocks {
			if b.ToolName == "ask_user" || b.ToolName == "plan_enter" || b.ToolName == "plan_exit" || b.ToolName == "task" {
				hasSequential = true
				break
			}
		}

		var toolResults []domain.ContentBlock
		if hasSequential {
			// Sequential path: ask_user requires blocking for user input
			for _, b := range toolUseBlocks {
				a.mu.Lock()
				if a.cancelled {
					a.mu.Unlock()
					return
				}
				a.mu.Unlock()

				onEvent(Event{
					Kind:      EventToolStart,
					ToolUseID: b.ToolUseID,
					ToolName:  b.ToolName,
				})

				var result string
				var isError bool

				if b.ToolName == "ask_user" {
					question, _ := b.ToolInput["question"].(string)
					if question == "" {
						question = "The agent wants your input."
					}
					respCh := make(chan string, 1)
					onEvent(Event{
						Kind:        EventAskUser,
						AskPrompt:   question,
						AskResponse: respCh,
					})
					// Block until user responds or agent is cancelled
					select {
					case answer := <-respCh:
						result = answer
						isError = false
					case <-func() chan struct{} {
						ch := make(chan struct{})
						go func() {
							for {
								a.mu.Lock()
								cancelled := a.cancelled
								a.mu.Unlock()
								if cancelled {
									close(ch)
									return
								}
								// Poll cancellation
								select {
								case <-time.After(100 * time.Millisecond):
								case <-ch:
									return
								}
							}
						}()
						return ch
					}():
						return
					}
				} else {
					result, isError = ExecuteToolCall(b, toolCtx)
				}

				onEvent(Event{
					Kind:        EventToolDone,
					ToolUseID:   b.ToolUseID,
					ToolName:    b.ToolName,
					ToolResult:  result,
					ToolIsError: isError,
				})

				toolResults = append(toolResults, domain.ContentBlock{
					Type:       "tool_result",
					ToolUseID:  b.ToolUseID,
					ToolName:   b.ToolName,
					ToolResult: result,
					IsError:    isError,
				})
			}
		} else {
			// Parallel path: execute all tools concurrently
			toolResults = make([]domain.ContentBlock, len(toolUseBlocks))
			var wg sync.WaitGroup

			for i, b := range toolUseBlocks {
				a.mu.Lock()
				if a.cancelled {
					a.mu.Unlock()
					return
				}
				a.mu.Unlock()

				wg.Add(1)
				go func(idx int, block domain.ContentBlock) {
					defer wg.Done()

					onEvent(Event{
						Kind:      EventToolStart,
						ToolUseID: block.ToolUseID,
						ToolName:  block.ToolName,
					})

					result, isError := ExecuteToolCall(block, toolCtx)

					onEvent(Event{
						Kind:        EventToolDone,
						ToolUseID:   block.ToolUseID,
						ToolName:    block.ToolName,
						ToolResult:  result,
						ToolIsError: isError,
					})

					toolResults[idx] = domain.ContentBlock{
						Type:       "tool_result",
						ToolUseID:  block.ToolUseID,
						ToolName:   block.ToolName,
						ToolResult: result,
						IsError:    isError,
					}
				}(i, b)
			}
			wg.Wait()
		}

		// 3f. Persist tool results as user message
		toolMsg := domain.TranscriptMessage{
			Role:   "user",
			Blocks: toolResults,
		}

		a.mu.Lock()
		a.messages = append(a.messages, toolMsg)
		a.mu.Unlock()

		if a.store != nil && a.session != nil {
			if err := a.store.AppendMessageBlocks(a.session.ID, "user", toolResults, 0); err != nil {
				fmt.Fprintf(os.Stderr, "agent: persist tool results: %v\n", err)
			}
		}

		// 3g. Compact if needed before looping back
		a.compactIfNeeded(onEvent)
	}
}
