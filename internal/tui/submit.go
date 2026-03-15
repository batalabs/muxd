package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
	"github.com/batalabs/muxd/internal/docread"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/tools"
)

// submit sends the user's message or handles a slash command.
func (m Model) submit(trimmed string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(trimmed, "/") {
		return m.handleSlashCommand(trimmed)
	}

	// Skip model/API key checks when connected to a remote daemon (it has its own config)
	isRemote := m.hubBaseURL != "" || m.Store == nil
	if !isRemote {
		// Require model selection before chatting
		if m.Prefs.Model == "" {
			return m, PrintToScrollback(m.renderError("No model selected. Use /config set model <name> (e.g. /config set model claude-sonnet)"))
		}

		// Check for API key before sending to the daemon
		provName := ""
		if m.Provider != nil {
			provName = m.Provider.Name()
		}
		if provName != "ollama" && provName != "" {
			// Re-resolve from prefs in case the key was set after startup
			if key, err := config.LoadProviderAPIKey(m.Prefs, provName); err == nil {
				m.APIKey = key
			}
			if m.APIKey == "" {
				hint := fmt.Sprintf("No API key set. Use /config set %s.api_key <key>", provName)
				return m, PrintToScrollback(m.renderError(hint))
			}
		} else if m.APIKey == "" && provName == "" {
			hint := "No API key set. Use /config set your_provider.api_key <key>"
			return m, PrintToScrollback(m.renderError(hint))
		}
	}

	// Detect image paths in the input and build multi-block message if found.
	imgPaths, remainingText := tools.ExtractImagePaths(trimmed)

	// Detect document paths and inline their extracted text into the message.
	docPaths, remainingText2 := tools.ExtractDocPaths(remainingText)
	remainingText = remainingText2
	for _, docPath := range docPaths {
		text, err := docread.Extract(docPath)
		if err != nil {
			continue // skip unreadable docs
		}
		header := fmt.Sprintf("[Document: %s]\n", filepath.Base(docPath))
		remainingText = header + text + "\n\n" + remainingText
	}

	var userMsg domain.TranscriptMessage
	var images []daemon.SubmitImage

	if len(imgPaths) > 0 {
		var blocks []domain.ContentBlock
		for _, imgPath := range imgPaths {
			data, err := os.ReadFile(imgPath)
			if err != nil {
				return m, PrintToScrollback(m.renderError(fmt.Sprintf("Failed to read image: %v", err)))
			}
			if len(data) > 20*1024*1024 {
				return m, PrintToScrollback(m.renderError(fmt.Sprintf("Image too large (max 20MB): %s", filepath.Base(imgPath))))
			}
			mediaType := tools.MediaTypeFromExt(imgPath)
			if mediaType == "" {
				continue
			}
			b64 := base64.StdEncoding.EncodeToString(data)
			blocks = append(blocks, domain.ContentBlock{
				Type:       "image",
				MediaType:  mediaType,
				Base64Data: b64,
				ImagePath:  filepath.Base(imgPath),
			})
			images = append(images, daemon.SubmitImage{
				Path:      filepath.Base(imgPath),
				MediaType: mediaType,
				Data:      b64,
			})
		}
		if remainingText != "" {
			blocks = append(blocks, domain.ContentBlock{Type: "text", Text: remainingText})
		}
		userMsg = domain.TranscriptMessage{Role: "user", Blocks: blocks}
		if remainingText != "" {
			userMsg.Content = remainingText
		}
	} else {
		userMsg = domain.TranscriptMessage{Role: "user", Content: trimmed}
	}

	m.messages = append(m.messages, userMsg)
	m.history = append(m.history, trimmed)
	m.historyIdx = -1
	m.historyDraft = ""
	m.setInput("")
	m.thinking = true
	m.streaming = false
	m.streamBuf = ""
	m.streamFlushedLen = 0
	m.turnToolCount = 0
	m.turnFilesChanged = nil
	m.turnStartTime = time.Now()
	m.turnCurrentTool = ""
	m.appendRuntimeLog("submit: " + summarizeForLog(trimmed))

	submitText := trimmed
	if len(images) > 0 {
		submitText = remainingText
	}

	m.lastSubmitText = submitText
	m.lastSubmitImages = images

	formatted := FormatMessageForScrollback(userMsg, m.width)
	cmds := []tea.Cmd{
		PrintToScrollback(formatted),
		StreamViaDaemon(m.Daemon, m.Session.ID, submitText, images),
		m.spinner.Tick,
	}
	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Daemon streaming
// ---------------------------------------------------------------------------

// StreamViaDaemon sends a message to the daemon via HTTP SSE and dispatches
// events to the TUI via Prog.Send().
func StreamViaDaemon(d *daemon.DaemonClient, sessionID, text string, images []daemon.SubmitImage) tea.Cmd {
	return func() tea.Msg {
		if d == nil {
			return StreamDoneMsg{Err: fmt.Errorf("no daemon connection")}
		}
		err := d.Submit(sessionID, text, images, func(evt daemon.SSEEvent) {
			if Prog == nil {
				return
			}
			switch evt.Type {
			case "delta":
				Prog.Send(StreamDeltaMsg{Text: evt.DeltaText})
			case "tool_start":
				Prog.Send(ToolStatusMsg{Name: evt.ToolName, Status: "running", Input: evt.ToolInput})
			case "tool_done":
				Prog.Send(ToolResultMsg{Name: evt.ToolName, Result: evt.ToolResult, IsError: evt.ToolIsError})
			case "stream_done":
				Prog.Send(StreamDoneMsg{
					InputTokens:              evt.InputTokens,
					OutputTokens:             evt.OutputTokens,
					CacheCreationInputTokens: evt.CacheCreationInputTokens,
					CacheReadInputTokens:     evt.CacheReadInputTokens,
					StopReason:               evt.StopReason,
				})
			case "ask_user":
				Prog.Send(AskUserMsg{Prompt: evt.AskPrompt, AskID: evt.AskID})
			case "turn_done":
				Prog.Send(TurnDoneMsg{StopReason: evt.StopReason})
			case "retrying":
				Prog.Send(RetryingMsg{
					Attempt: evt.RetryAttempt,
					WaitMs:  evt.RetryWaitMs,
					Message: evt.RetryMessage,
				})
			case "error":
				Prog.Send(StreamDoneMsg{Err: fmt.Errorf("%s", evt.ErrorMsg)})
			case "compacted":
				Prog.Send(CompactedMsg{ModelUsed: evt.ModelUsed})
			case "titled":
				Prog.Send(TitledMsg{Title: evt.Title, Tags: evt.Tags, ModelUsed: evt.ModelUsed})
			}
		})
		if err != nil {
			return StreamDoneMsg{Err: err}
		}
		return nil
	}
}

// SendAskResponseCmd sends the user's answer to the daemon for a pending ask_user.
func SendAskResponseCmd(d *daemon.DaemonClient, sessionID, askID, answer string) tea.Cmd {
	return func() tea.Msg {
		if d == nil {
			return nil
		}
		if err := d.SendAskResponse(sessionID, askID, answer); err != nil {
			fmt.Fprintf(os.Stderr, "tui: send ask response: %v\n", err)
		}
		return nil
	}
}

// ConsultCmd sends a consult request to the daemon and returns a ConsultResponseMsg.
func ConsultCmd(d *daemon.DaemonClient, sessionID, question string) tea.Cmd {
	return func() tea.Msg {
		if d == nil {
			return ConsultResponseMsg{Model: "", Text: "No daemon connection."}
		}
		model, response, err := d.Consult(sessionID, question)
		if err != nil {
			return ConsultResponseMsg{Model: "", Text: "Consult error: " + err.Error()}
		}
		return ConsultResponseMsg{Model: model, Text: response}
	}
}
