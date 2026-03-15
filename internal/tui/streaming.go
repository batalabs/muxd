package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/batalabs/muxd/internal/domain"
)

// flushStreamContent checks whether enough complete paragraphs have
// accumulated in the unflushed portion of streamBuf.
func (m *Model) flushStreamContent() tea.Cmd {
	unflushed := m.streamBuf[m.streamFlushedLen:]
	n := FindSafeFlushPoint(unflushed)
	if n == 0 {
		return nil
	}

	contentWidth := max(20, m.width-4)
	text := unflushed[:n]
	lines := RenderAssistantLines(text, contentWidth-2)

	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		if i == 0 && m.streamFlushedLen == 0 {
			b.WriteString(AsstIconStyle.Render("\u25cf ") + line)
		} else {
			b.WriteString("  " + line)
		}
	}

	m.streamFlushedLen += n
	return PrintToScrollback(b.String())
}

func (m Model) handleStreamDone(msg StreamDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.thinking = false
		m.streaming = false
		m.streamBuf = ""
		m.streamFlushedLen = 0
		m.appendRuntimeLog("stream_error: " + msg.Err.Error())

		// Auto-recover: create a new session and retry the submit.
		if strings.Contains(msg.Err.Error(), "session not found") || strings.Contains(msg.Err.Error(), "no rows in result set") {
			if m.Daemon != nil && m.lastSubmitText != "" {
				cwd := MustGetwd()
				sessionID, err := m.Daemon.CreateSession(cwd, m.modelID)
				if err == nil {
					if sess, err := m.Daemon.GetSession(sessionID); err == nil {
						m.appendRuntimeLog("session_recovery: new session " + sessionID)
						m.Session = sess
						m.messages = nil
						m.inputTokens = 0
						m.outputTokens = 0
						m.titled = false
						notice := WelcomeStyle.Render("Session recovered — created new session " + sessionID[:8] + ".")
						m.thinking = true
						return m, tea.Batch(
							PrintToScrollback(notice),
							StreamViaDaemon(m.Daemon, m.Session.ID, m.lastSubmitText, m.lastSubmitImages),
							m.spinner.Tick,
						)
					}
				}
				m.appendRuntimeLog("session_recovery_failed: " + err.Error())
			}
			return m, PrintToScrollback(m.renderError("Error: " + msg.Err.Error() + "\nhint: session may have been lost. Use /new to start a new session."))
		}

		errText := "Error: " + msg.Err.Error()
		return m, PrintToScrollback(m.renderError(errText))
	}

	// Update token counts
	m.inputTokens += msg.InputTokens
	m.outputTokens += msg.OutputTokens
	m.lastInputTokens = msg.InputTokens
	m.lastOutputTokens = msg.OutputTokens
	m.cacheCreationInputTokens += msg.CacheCreationInputTokens
	m.cacheReadInputTokens += msg.CacheReadInputTokens
	m.lastCacheCreationInputTokens = msg.CacheCreationInputTokens
	m.lastCacheReadInputTokens = msg.CacheReadInputTokens
	m.appendRuntimeLog(fmt.Sprintf(
		"stream_done: stop=%s in=%d out=%d cache_create=%d cache_read=%d",
		msg.StopReason,
		msg.InputTokens,
		msg.OutputTokens,
		msg.CacheCreationInputTokens,
		msg.CacheReadInputTokens,
	))

	// Flush any remaining streamed content to viewLines before clearing
	var cmd tea.Cmd
	if m.streamBuf != "" {
		unflushed := m.streamBuf[m.streamFlushedLen:]
		if strings.TrimSpace(unflushed) != "" {
			contentWidth := max(20, m.width-4)
			lines := RenderAssistantLines(unflushed, contentWidth-2)
			var b strings.Builder
			for i, line := range lines {
				if i > 0 {
					b.WriteString("\n")
				}
				if i == 0 && m.streamFlushedLen == 0 {
					b.WriteString(AsstIconStyle.Render("\u25cf ") + line)
				} else {
					b.WriteString("  " + line)
				}
			}
			cmd = PrintToScrollback(b.String())
		}
	}

	// Reset streaming state for next API call in the agent loop
	m.streaming = false
	m.streamBuf = ""
	m.streamFlushedLen = 0
	m.toolStatus = "Thinking..."

	return m, cmd
}

// ---------------------------------------------------------------------------
// Context compaction
// ---------------------------------------------------------------------------

const (
	// CompactKeepTail is the number of recent messages to keep during compaction.
	CompactKeepTail = 20
)

// CompactMessages trims the middle of a conversation to fit within token limits.
func CompactMessages(msgs []domain.TranscriptMessage) []domain.TranscriptMessage {
	if len(msgs) <= CompactKeepTail+2 {
		return msgs
	}

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

	tailStart := len(msgs) - CompactKeepTail
	if tailStart <= headEnd {
		return msgs
	}
	for tailStart < len(msgs) && msgs[tailStart].Role != "user" {
		tailStart++
	}
	if tailStart >= len(msgs) {
		return msgs
	}
	tail := msgs[tailStart:]

	dropped := tailStart - headEnd
	notice := fmt.Sprintf("[%d earlier messages compacted to save context]", dropped)

	compacted := make([]domain.TranscriptMessage, 0, len(head)+2+len(tail))
	compacted = append(compacted, head...)
	compacted = append(compacted,
		domain.TranscriptMessage{Role: "user", Content: notice},
		domain.TranscriptMessage{Role: "assistant", Content: "Understood. I'll continue with the context available."},
	)
	compacted = append(compacted, tail...)
	return compacted
}
