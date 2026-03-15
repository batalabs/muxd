package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/hub"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"
)

// ---------------------------------------------------------------------------
// Bubble Tea message types
// ---------------------------------------------------------------------------

// StreamDeltaMsg carries a streaming text delta from the daemon.
type StreamDeltaMsg struct {
	Text string
}

// StreamDoneMsg signals that one API call finished.
type StreamDoneMsg struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	StopReason               string
	Err                      error
}

// ScrollbackDoneMsg signals that a scrollback print completed (legacy, unused).
type ScrollbackDoneMsg struct{}

// AppendViewMsg adds rendered text to the view message buffer.
type AppendViewMsg struct {
	Text string
}

// BatchViewMsg adds multiple rendered text blocks to the view buffer at once.
type BatchViewMsg struct {
	Lines []string
}

// HistoryLoadedMsg is sent after replaying a resumed session's messages.
type HistoryLoadedMsg struct{}

// ToolStatusMsg is sent when a tool starts executing on the server.
type ToolStatusMsg struct {
	Name   string
	Status string
	Input  map[string]any
}

// ToolResultMsg is sent when a tool finishes executing on the server.
type ToolResultMsg struct {
	Name    string
	Result  string
	IsError bool
}

// TurnDoneMsg signals that the full agent turn is complete (server-driven).
type TurnDoneMsg struct {
	StopReason string
}

// MCPToolsMsg delivers MCP tool names fetched from the daemon.
type MCPToolsMsg struct {
	Names []string
}

// CompactedMsg signals that the server compacted the context.
type CompactedMsg struct {
	ModelUsed string
}

// AskUserMsg is sent when the agent's ask_user tool needs user input.
type AskUserMsg struct {
	Prompt string
	AskID  string
}

// TitledMsg signals that the session title and tags were generated.
type TitledMsg struct {
	Title     string
	Tags      string
	ModelUsed string
}

// RetryingMsg signals that the agent is retrying after a rate limit error.
type RetryingMsg struct {
	Attempt int
	WaitMs  int
	Message string
}

// GitAvailableMsg reports git repo availability.
type GitAvailableMsg struct {
	Available bool
	RepoRoot  string
}

// UndoDoneMsg reports undo completion.
type UndoDoneMsg struct {
	RestoredTurn int
	Err          error
}

// RedoDoneMsg reports redo completion.
type RedoDoneMsg struct {
	RestoredTurn int
	Err          error
}

// ShellResultMsg carries the result of a shell command execution.
type ShellResultMsg struct {
	Output string
	Err    error
}

// HubSyncMsg signals that new memory facts were synced from the hub.
type HubSyncMsg struct {
	Count int
}

// ConsultResponseMsg carries a second-opinion response from the consult model.
type ConsultResponseMsg struct {
	Model string
	Text  string
}

// Checkpoint represents a snapshot of the working tree.
type Checkpoint struct {
	TurnNumber int
	SHA        string
	IsClean    bool
}

// ---------------------------------------------------------------------------
// Bubble Tea model -- hybrid inline mode
// ---------------------------------------------------------------------------

// Model is the Bubble Tea model for the TUI.
type Model struct {
	width                        int
	height                       int
	version                      string
	modelID                      string
	modelLabel                   string
	input                        string
	inputCursor                  int
	thinking                     bool
	streaming                    bool
	streamBuf                    string
	streamFlushedLen             int
	inputTokens                  int
	outputTokens                 int
	lastInputTokens              int
	lastOutputTokens             int
	cacheCreationInputTokens     int
	cacheReadInputTokens         int
	lastCacheCreationInputTokens int
	lastCacheReadInputTokens     int
	messages                     []domain.TranscriptMessage
	spinner                      spinner.Model

	history      []string
	historyIdx   int
	historyDraft string

	Store    *store.Store
	Session  *domain.Session
	resuming bool
	titled   bool

	// Daemon client (TUI communicates via HTTP)
	Daemon       *daemon.DaemonClient
	pendingAskID string

	// Tool status display
	toolStatus       string
	turnToolCount    int
	turnFilesChanged map[string]bool
	turnStartTime    time.Time
	turnCurrentTool  string
	turnLastAction   string // human-readable summary of last completed action

	Prefs    config.Preferences
	Provider provider.Provider
	APIKey   string

	// Ask-user state: agent paused waiting for input
	pendingAsk bool

	// Autocomplete state
	completions   []string
	completionIdx int
	completionOn  bool

	// Checkpoint/undo state
	gitAvailable bool
	gitRepoRoot  string
	checkpoints  []Checkpoint
	redoStack    []Checkpoint

	// Session picker overlay
	picker *SessionPicker
	// Node picker overlay (hub connections)
	nodePicker *NodePicker
	// Tool picker overlay
	toolPicker *ToolPicker
	// Config picker overlay
	configPicker *ConfigPicker
	// Emoji picker overlay
	emojiPicker *EmojiPicker

	// MCP tool names (fetched from daemon at startup)
	mcpToolNames []string

	// Hub connection state (non-empty when connected via --remote to a hub)
	hubBaseURL string
	hubToken   string

	// Rendered message blocks displayed in the View (replaces Prog.Println scrollback)
	viewLines []string

	// Runtime diagnostics log path (best effort, may be empty).
	runtimeLogPath string

	// Paste detection: rapid keystrokes (< 5ms apart) indicate pasted text.
	lastKeypressTime time.Time

	// Last submit payload for session-recovery retry.
	lastSubmitText   string
	lastSubmitImages []daemon.SubmitImage

	// Shell mode: interactive shell session
	shellActive      bool
	shellInput       string
	shellInputCursor int
	shellCwd         string
	shellHistory     []string
	shellHistoryIdx  int
	shellLastOK      bool // true when last command exited 0
}

// InitialModel creates the initial Bubble Tea model.
func InitialModel(d *daemon.DaemonClient, version, modelLabel, modelID string, st *store.Store, session *domain.Session, resuming bool, prov provider.Provider, prefs config.Preferences, apiKey string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	m := Model{
		Daemon:         d,
		version:        version,
		modelID:        modelID,
		modelLabel:     modelLabel,
		spinner:        sp,
		historyIdx:     -1,
		Store:          st,
		Session:        session,
		resuming:       resuming,
		Prefs:          prefs,
		Provider:       prov,
		APIKey:         apiKey,
		runtimeLogPath: defaultRuntimeLogPath(),
	}
	if session != nil {
		m.inputTokens = session.InputTokens
		m.outputTokens = session.OutputTokens
	}
	if !resuming {
		m.viewLines = []string{WelcomeStyle.Render("Welcome to muxd. One prompt away from wizardry.")}
	}
	m.appendRuntimeLog("tui initialized")
	return m
}

// SetHubConnection configures the model for hub mode, enabling the node
// picker on startup. Call this before passing the model to tea.NewProgram.
func (m *Model) SetHubConnection(baseURL, token string) {
	m.hubBaseURL = baseURL
	m.hubToken = token
	m.viewLines = []string{WelcomeStyle.Render("Connecting to hub...")}
}

// Init initializes the Bubble Tea model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, CheckGitRepo()}

	if m.resuming {
		cmds = append(cmds, m.loadSessionHistory())
	}

	// If connected to a hub without a session, fetch nodes on startup.
	if m.hubBaseURL != "" && m.Session == nil {
		cmds = append(cmds, m.openNodePicker())
	}

	// Fetch MCP tool names from daemon in background.
	if m.Daemon != nil && m.Session != nil {
		d := m.Daemon
		cmds = append(cmds, func() tea.Msg {
			resp, err := d.GetMCPTools()
			if err != nil || len(resp.Tools) == 0 {
				return MCPToolsMsg{}
			}
			return MCPToolsMsg{Names: resp.Tools}
		})
	}

	return tea.Batch(cmds...)
}

// loadSessionHistory replays persisted messages into the view buffer.
func (m Model) loadSessionHistory() tea.Cmd {
	sessionID := m.Session.ID
	st := m.Store
	return func() tea.Msg {
		msgs, err := st.GetMessages(sessionID)
		if err != nil || len(msgs) == 0 {
			return BatchViewMsg{Lines: []string{
				WelcomeStyle.Render("Welcome to muxd. One prompt away from wizardry."),
			}}
		}

		var lines []string
		lines = append(lines, WelcomeStyle.Render(fmt.Sprintf("  Resumed: %s  (%d messages)", st.SessionTitle(sessionID), len(msgs))))

		width := 80
		for _, msg := range msgs {
			if msg.Role == "system" {
				continue
			}
			lines = append(lines, FormatBlockMessage(msg, width))
		}

		return historyBatchMsg{lines: lines}
	}
}

// historyBatchMsg is an internal message that carries both view lines and
// signals that history loading is complete.
type historyBatchMsg struct {
	lines []string
}

// Update handles Bubble Tea messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case PasteMsg:
		return m.handlePaste(msg)

	case ClipboardWriteMsg:
		return m.handleClipboardWrite(msg)

	case StreamDeltaMsg:
		m.streaming = true
		// Strip leading newlines at start of response to keep bullet on same line as text.
		if m.streamFlushedLen == 0 && m.streamBuf == "" {
			msg.Text = strings.TrimLeft(msg.Text, "\n\r")
		}
		m.streamBuf += msg.Text
		return m, m.flushStreamContent()

	case StreamDoneMsg:
		return m.handleStreamDone(msg)

	case ToolStatusMsg:
		return m.handleToolStatus(msg)

	case ToolResultMsg:
		return m.handleToolResult(msg)

	case TurnDoneMsg:
		return m.handleTurnDone(msg)

	case CompactedMsg:
		return m, nil

	case historyBatchMsg:
		var historyCmd tea.Cmd
		if len(msg.lines) > 0 {
			historyCmd = PrintToScrollback(strings.Join(msg.lines, "\n\n"))
		}
		next, loadedCmd := m.handleHistoryLoaded()
		if mm, ok := next.(Model); ok {
			m = mm
		}
		return m, tea.Batch(historyCmd, loadedCmd)

	case MCPToolsMsg:
		m.mcpToolNames = msg.Names
		return m, nil

	case HistoryLoadedMsg:
		return m.handleHistoryLoaded()

	case TitledMsg:
		m.Session.Title = msg.Title
		m.Session.Tags = msg.Tags
		m.titled = true
		return m, nil

	case RetryingMsg:
		m.toolStatus = msg.Message
		m.appendRuntimeLog("retrying: " + msg.Message)
		return m, nil

	case AskUserMsg:
		return m.handleAskUser(msg)

	case GitAvailableMsg:
		m.gitAvailable = msg.Available
		m.gitRepoRoot = msg.RepoRoot
		return m, nil

	case UndoDoneMsg:
		return m.handleUndoDone(msg)

	case RedoDoneMsg:
		return m.handleRedoDone(msg)

	case ShellResultMsg:
		return m.handleShellResult(msg)

	case HubSyncMsg:
		text := fmt.Sprintf("[hub] synced %d memory facts", msg.Count)
		return m, PrintToScrollback(HubStyle.Render(text))

	case ConsultResponseMsg:
		formatted := FormatConsultResponse(msg.Model, msg.Text, m.width)
		return m, PrintToScrollback(formatted)

	case spinner.TickMsg:
		if m.thinking {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case AppendViewMsg:
		m.viewLines = append(m.viewLines, msg.Text)
		return m, nil

	case BatchViewMsg:
		m.viewLines = append(m.viewLines, msg.Lines...)
		return m, nil

	case ScrollbackDoneMsg:
		return m, nil

	case SessionPickerMsg:
		if msg.Err != nil {
			return m, PrintToScrollback(m.renderError("Failed to load sessions: " + msg.Err.Error()))
		}
		if len(msg.Sessions) == 0 {
			return m, PrintToScrollback(FooterMeta.Render("No sessions found for this project."))
		}
		m.picker = NewSessionPicker(msg.Sessions)
		return m, nil

	case NodePickerMsg:
		if msg.Err != nil {
			return m, PrintToScrollback(m.renderError("Failed to load nodes: " + msg.Err.Error()))
		}
		if len(msg.Nodes) == 0 {
			return m, PrintToScrollback(FooterMeta.Render("No nodes registered with hub."))
		}
		m.nodePicker = NewNodePicker(msg.Nodes)
		return m, nil

	case BranchDoneMsg:
		return m.handleBranchDone(msg)

	default:
		return m, nil
	}
}

// View renders the active area at the bottom of the terminal.
func (m Model) View() string {
	var b strings.Builder

	// Render node picker overlay if active (hub connections)
	if m.nodePicker.IsActive() {
		b.WriteString(m.nodePicker.View(m.width))
		return b.String()
	}
	// Render session picker overlay if active
	if m.picker.IsActive() {
		b.WriteString(m.picker.View(m.width))
		return b.String()
	}
	if m.toolPicker.IsActive() {
		b.WriteString(m.toolPicker.View(m.width))
		return b.String()
	}
	if m.configPicker.IsActive() {
		b.WriteString(m.configPicker.View(m.width))
		return b.String()
	}
	if m.emojiPicker.IsActive() {
		b.WriteString(m.emojiPicker.View(m.width))
		return b.String()
	}

	// Calculate available width for text wrapping
	promptWidth := 2
	availWidth := m.width - promptWidth
	if availWidth < 10 {
		availWidth = 10
	}

	// Render shell mode
	if m.shellActive {
		// Shorten cwd for display
		cwd := m.shellCwd
		if len(cwd) > 30 {
			cwd = "..." + cwd[len(cwd)-27:]
		}
		// Build header: muxd shell | branch* | exit to return | cwd
		headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
		header := "muxd shell"
		if gitInfo := shellGitInfo(m.shellCwd); gitInfo != "" {
			branchColor := "114" // green
			if strings.HasSuffix(gitInfo, "*") {
				branchColor = "214" // yellow/orange for dirty
			}
			header += " | " + lipgloss.NewStyle().Foreground(lipgloss.Color(branchColor)).Render(gitInfo)
		}
		header += headerStyle.Render(" | exit to return | ") + cwd
		b.WriteString(headerStyle.Render(header) + "\n\n")

		// Color prompt based on last command exit status
		promptColor := "114" // green
		if !m.shellLastOK {
			promptColor = "196" // red
		}
		promptStr := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor)).Render("❯")

		shellLines := strings.Split(withInlineCursor(m.shellInput, m.shellInputCursor), "\n")
		first := true
		for _, line := range shellLines {
			wrapped := hardWrapLine(line, availWidth)
			for _, wl := range wrapped {
				if first {
					b.WriteString(promptStr + " " + wl)
					first = false
				} else {
					b.WriteString("\n  " + wl)
				}
			}
		}
		b.WriteString("\n")
		return b.String()
	}

	if m.pendingAsk {
		b.WriteString(ThinkingStyle.Render("Agent is waiting for your response...") + "\n\n")
	}

	if m.thinking {
		b.WriteString(ThinkingStyle.Render(m.spinner.View()+" "+m.buildActivityStatus()) + "\n\n")
	}

	// Multi-line input with inline cursor and visual line wrapping.
	inputLines := strings.Split(withInlineCursor(m.input, m.inputCursor), "\n")
	first := true
	for _, line := range inputLines {
		wrapped := hardWrapLine(line, availWidth)
		for _, wl := range wrapped {
			if first {
				b.WriteString(PromptStyle.Render("\u276f ") + InputStyle.Render(wl))
				first = false
			} else {
				b.WriteString("\n" + PromptStyle.Render("  ") + InputStyle.Render(wl))
			}
		}
	}

	if m.completionOn && len(m.completions) > 0 {
		b.WriteString("\n")
		b.WriteString(RenderCompletionMenu(m.completions, m.completionIdx, max(40, m.width)))
	}

	b.WriteString("\n\n")
	disabledCount := len(m.Prefs.DisabledToolsSet())
	prefix := ""
	indent := ""
	if m.Prefs.FooterEmoji != "" {
		prefix = m.Prefs.FooterEmoji + " "
		indent = "   "
	}
	footerParts := []string{fmt.Sprintf("%smuxd %s", prefix, m.version)}
	if m.Prefs.Model != "" {
		footerParts = append(footerParts, m.modelLabel)
	}
	if disabledCount > 0 {
		footerParts = append(footerParts, fmt.Sprintf("tools off: %d", disabledCount))
	}
	b.WriteString(FooterHead.Render(strings.Join(footerParts, " \u00b7 ")))
	if m.Prefs.FooterTokens {
		b.WriteString("\n")
		totalTokens := m.inputTokens + m.outputTokens
		tokenStr := fmt.Sprintf("%ssession tokens: %.1fk", indent, float64(totalTokens)/1000.0)
		if m.Prefs.FooterCost {
			sessionCost := provider.ModelCost(m.modelID, m.inputTokens, m.outputTokens)
			turnCost := provider.ModelCost(m.modelID, m.lastInputTokens, m.lastOutputTokens)
			if sessionCost > 0 {
				tokenStr += fmt.Sprintf(" \u00b7 session $%.4f \u00b7 last turn $%.4f", sessionCost, turnCost)
			}
			if m.cacheCreationInputTokens > 0 || m.cacheReadInputTokens > 0 {
				cacheAdjSession := provider.ModelCostWithCache(
					m.modelID,
					m.inputTokens,
					m.outputTokens,
					m.cacheCreationInputTokens,
					m.cacheReadInputTokens,
				)
				if cacheAdjSession > 0 {
					tokenStr += fmt.Sprintf(" \u00b7 cache-adjusted session $%.4f", cacheAdjSession)
				}
			}
			if m.lastCacheCreationInputTokens > 0 || m.lastCacheReadInputTokens > 0 {
				cacheAdjTurn := provider.ModelCostWithCache(
					m.modelID,
					m.lastInputTokens,
					m.lastOutputTokens,
					m.lastCacheCreationInputTokens,
					m.lastCacheReadInputTokens,
				)
				if cacheAdjTurn > 0 {
					tokenStr += fmt.Sprintf(" \u00b7 cache-adjusted turn $%.4f", cacheAdjTurn)
				}
			}
		}
		b.WriteString(FooterTokens.Render(tokenStr))
	}
	if m.Prefs.FooterCwd {
		b.WriteString("\n")
		b.WriteString(FooterMeta.Render(fmt.Sprintf("%scwd: %s", indent, MustGetwd())))
	}
	if m.Prefs.FooterSession && m.Session != nil {
		b.WriteString("\n")
		sessionLine := fmt.Sprintf("%ssession: %s", indent, m.Session.ID[:8])
		if m.Session.Title != "New Session" {
			sessionLine = fmt.Sprintf("%ssession: %s", indent, m.Session.Title)
		}
		b.WriteString(FooterMeta.Render(sessionLine))
	}
	b.WriteString("\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// Key handler
// ---------------------------------------------------------------------------

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Route to node picker when active (hub connections).
	if m.nodePicker.IsActive() {
		return m.handleNodePickerKey(msg)
	}
	// Route to picker when active.
	if m.picker.IsActive() {
		return m.handlePickerKey(msg)
	}
	if m.toolPicker.IsActive() {
		return m.handleToolPickerKey(msg)
	}
	if m.configPicker.IsActive() {
		return m.handleConfigPickerKey(msg)
	}
	if m.emojiPicker.IsActive() {
		return m.handleEmojiPickerKey(msg)
	}

	// Route to shell mode when active.
	if m.shellActive {
		return m.handleShellKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.completionOn {
			m.dismissCompletions()
			return m, nil
		}
		if m.pendingAsk {
			m.pendingAsk = false
			m.pendingAskID = ""
			m.toolStatus = ""
			if m.Daemon != nil {
				go func() { _ = m.Daemon.Cancel(m.Session.ID) }()
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop canceled."))
		}
		if m.thinking {
			m.thinking = false
			m.streaming = false
			m.toolStatus = ""
			if m.Daemon != nil {
				go func() { _ = m.Daemon.Cancel(m.Session.ID) }()
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop canceled."))
		}
		return m, tea.Quit

	case tea.KeyEsc:
		if m.completionOn {
			m.dismissCompletions()
			return m, nil
		}
		if m.pendingAsk {
			m.pendingAsk = false
			m.pendingAskID = ""
			m.toolStatus = ""
			if m.Daemon != nil {
				go func() { _ = m.Daemon.Cancel(m.Session.ID) }()
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop canceled."))
		}
		if m.thinking {
			m.thinking = false
			m.streaming = false
			m.toolStatus = ""
			if m.Daemon != nil {
				go func() { _ = m.Daemon.Cancel(m.Session.ID) }()
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop canceled."))
		}
		return m, tea.Quit

	case tea.KeyTab:
		if m.thinking {
			return m, nil
		}
		if strings.HasPrefix(m.input, "/") {
			if !m.completionOn {
				m.completions = ComputeCompletions(m.input, nil)
				if len(m.completions) > 0 {
					m.completionOn = true
					m.completionIdx = 0
					m.setInput(m.completions[0])
				}
			} else if len(m.completions) > 0 {
				m.completionIdx = (m.completionIdx + 1) % len(m.completions)
				m.setInput(m.completions[m.completionIdx])
			}
		}
		return m, nil

	case tea.KeyShiftTab:
		if m.thinking {
			return m, nil
		}
		if m.completionOn && len(m.completions) > 0 {
			m.completionIdx = (m.completionIdx - 1 + len(m.completions)) % len(m.completions)
			m.setInput(m.completions[m.completionIdx])
		}
		return m, nil

	case tea.KeyCtrlJ:
		if !m.thinking {
			m.dismissCompletions()
			m.insertInputAtCursor("\n")
			m.resetHistory()
		}
		return m, nil

	case tea.KeyEnter:
		if m.thinking {
			return m, nil
		}
		// Paste detection: bracketed paste flag OR rapid keystrokes (< 5ms)
		// indicate pasted text -treat Enter as a literal newline, not submit.
		now := time.Now()
		isPaste := msg.Paste || (!m.lastKeypressTime.IsZero() && now.Sub(m.lastKeypressTime) < 5*time.Millisecond)
		m.lastKeypressTime = now
		if isPaste {
			m.insertInputAtCursor("\n")
			return m, nil
		}
		// If agent is waiting for ask_user response, send answer via daemon
		if m.pendingAsk {
			answer := strings.TrimSpace(m.input)
			if answer == "" {
				return m, nil
			}
			askID := m.pendingAskID
			sessionID := m.Session.ID
			m.pendingAsk = false
			m.pendingAskID = ""
			m.setInput("")
			m.thinking = true
			m.toolStatus = "Thinking..."
			formatted := FormatMessageForScrollback(domain.TranscriptMessage{Role: "user", Content: answer}, m.width)
			return m, tea.Batch(
				PrintToScrollback(formatted),
				SendAskResponseCmd(m.Daemon, sessionID, askID, answer),
			)
		}
		if m.completionOn {
			selected := m.input
			m.dismissCompletions()
			if CommandExpectsArgs(selected) {
				m.setInput(selected + " ")
				return m, nil
			}
			m.setInput(selected)
		}
		trimmed := strings.TrimSpace(m.input)
		if trimmed == "" {
			m.setInput("")
			return m, nil
		}
		return m.submit(trimmed)

	case tea.KeyUp:
		if !m.thinking {
			m.dismissCompletions()
			m.browseHistoryBack()
		}
		return m, nil

	case tea.KeyDown:
		if !m.thinking {
			m.dismissCompletions()
			m.browseHistoryForward()
		}
		return m, nil

	case tea.KeyLeft:
		if !m.thinking {
			m.moveInputCursor(-1)
		}
		return m, nil

	case tea.KeyRight:
		if !m.thinking {
			m.moveInputCursor(1)
		}
		return m, nil

	case tea.KeyHome, tea.KeyCtrlA:
		if !m.thinking {
			m.moveInputCursorToStart()
		}
		return m, nil

	case tea.KeyEnd, tea.KeyCtrlE:
		if !m.thinking {
			m.moveInputCursorToEnd()
		}
		return m, nil

	case tea.KeyBackspace:
		if !m.thinking {
			m.dismissCompletions()
			if m.deleteInputBeforeCursor() {
				m.resetHistory()
			}
		}
		return m, nil

	case tea.KeyDelete:
		if !m.thinking {
			m.dismissCompletions()
			if m.deleteInputAtCursor() {
				m.resetHistory()
			}
		}
		return m, nil

	case tea.KeyCtrlV:
		if m.thinking {
			return m, nil
		}
		m.dismissCompletions()
		return m, ReadClipboardCmd()

	case tea.KeyInsert:
		if m.thinking {
			return m, nil
		}
		m.dismissCompletions()
		return m, ReadClipboardCmd()

	case tea.KeyCtrlY:
		return m, WriteClipboardCmd(m.lastAssistantMessage())

	case tea.KeyCtrlK:
		return m, WriteClipboardCmd(m.plainTranscript())

	case tea.KeyCtrlR:
		if !m.thinking {
			return m, m.openSessionPicker()
		}
		return m, nil

	case tea.KeySpace:
		if !m.thinking {
			m.dismissCompletions()
			m.insertInputAtCursor(" ")
			m.resetHistory()
			m.lastKeypressTime = time.Now()
		}
		return m, nil

	default:
		if !m.thinking {
			m.dismissCompletions()
			if msg.Paste && len(msg.Runes) > 0 {
				m.insertInputAtCursor(filterNulls(msg.Runes))
				m.resetHistory()
				m.lastKeypressTime = time.Now()
			} else if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				m.insertInputAtCursor(filterNulls(msg.Runes))
				m.resetHistory()
				m.lastKeypressTime = time.Now()
			}
		}
		return m, nil
	}
}

func (m *Model) dismissCompletions() {
	m.completionOn = false
	m.completions = nil
	m.completionIdx = 0
}

// filterNulls removes null bytes from runes before appending to input.
func filterNulls(runes []rune) string {
	clean := make([]rune, 0, len(runes))
	for _, r := range runes {
		if r != 0 {
			clean = append(clean, r)
		}
	}
	return string(clean)
}

// SessionPickerMsg carries sessions for the picker overlay.
type SessionPickerMsg struct {
	Sessions []domain.Session
	Err      error
}

// NodePickerMsg carries nodes for the node picker overlay.
type NodePickerMsg struct {
	Nodes []*hub.Node
	Err   error
}

// BranchDoneMsg signals that a session branch completed.
type BranchDoneMsg struct {
	Session *domain.Session
	Err     error
}

// ---------------------------------------------------------------------------
// Input history
// ---------------------------------------------------------------------------

func (m *Model) browseHistoryBack() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx == -1 {
		m.historyDraft = m.input
		m.historyIdx = len(m.history) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	}
	m.setInput(m.history[m.historyIdx])
}

func (m *Model) browseHistoryForward() {
	if m.historyIdx == -1 {
		return
	}
	if m.historyIdx < len(m.history)-1 {
		m.historyIdx++
		m.setInput(m.history[m.historyIdx])
	} else {
		m.historyIdx = -1
		m.setInput(m.historyDraft)
		m.historyDraft = ""
	}
}

func (m *Model) resetHistory() {
	m.historyIdx = -1
	m.historyDraft = ""
}

func (m *Model) setInput(s string) {
	m.input = s
	m.inputCursor = len([]rune(s))
}

func (m *Model) moveInputCursor(delta int) {
	max := len([]rune(m.input))
	m.inputCursor += delta
	if m.inputCursor < 0 {
		m.inputCursor = 0
	}
	if m.inputCursor > max {
		m.inputCursor = max
	}
}

func (m *Model) moveInputCursorToStart() {
	m.inputCursor = 0
}

func (m *Model) moveInputCursorToEnd() {
	m.inputCursor = len([]rune(m.input))
}

func (m *Model) insertInputAtCursor(s string) {
	if s == "" {
		return
	}
	r := []rune(m.input)
	if m.inputCursor < 0 {
		m.inputCursor = 0
	}
	if m.inputCursor > len(r) {
		m.inputCursor = len(r)
	}
	ins := []rune(s)
	out := make([]rune, 0, len(r)+len(ins))
	out = append(out, r[:m.inputCursor]...)
	out = append(out, ins...)
	out = append(out, r[m.inputCursor:]...)
	m.input = string(out)
	m.inputCursor += len(ins)
}

func (m *Model) deleteInputBeforeCursor() bool {
	r := []rune(m.input)
	if len(r) == 0 || m.inputCursor <= 0 {
		return false
	}
	if m.inputCursor > len(r) {
		m.inputCursor = len(r)
	}
	out := make([]rune, 0, len(r)-1)
	out = append(out, r[:m.inputCursor-1]...)
	out = append(out, r[m.inputCursor:]...)
	m.input = string(out)
	m.inputCursor--
	return true
}

func (m *Model) deleteInputAtCursor() bool {
	r := []rune(m.input)
	if len(r) == 0 {
		return false
	}
	if m.inputCursor < 0 {
		m.inputCursor = 0
	}
	if m.inputCursor >= len(r) {
		return false
	}
	out := make([]rune, 0, len(r)-1)
	out = append(out, r[:m.inputCursor]...)
	out = append(out, r[m.inputCursor+1:]...)
	m.input = string(out)
	return true
}

func withInlineCursor(input string, cursor int) string {
	r := []rune(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}
	with := make([]rune, 0, len(r)+1)
	with = append(with, r[:cursor]...)
	with = append(with, '█')
	with = append(with, r[cursor:]...)
	return string(with)
}

func hardWrapLine(line string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var lines []string
	for len(runes) > width {
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	lines = append(lines, string(runes))
	return lines
}

// CheckGitRepo detects whether the cwd is inside a git repo at startup.
func CheckGitRepo() tea.Cmd {
	return func() tea.Msg {
		root, ok := DetectGitRepo()
		return GitAvailableMsg{Available: ok, RepoRoot: root}
	}
}

// DetectGitRepo returns the repo root if the cwd is inside a git repo.
func DetectGitRepo() (string, bool) {
	root, err := GitRun("rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	return root, true
}

// GitRun executes a git command and returns trimmed stdout.
func GitRun(args ...string) (string, error) {
	return gitRun(args...)
}

// ---------------------------------------------------------------------------
// Git helpers
// ---------------------------------------------------------------------------

func gitRun(args ...string) (string, error) {
	c := exec.Command("git", args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ---------------------------------------------------------------------------
// Checkpoint / Undo / Redo (Tea.Cmd wrappers)
// ---------------------------------------------------------------------------

// RestoreCheckpoint undoes the current working tree state back to the given checkpoint.
func RestoreCheckpoint(cp Checkpoint, sessionPrefix string) tea.Cmd {
	return func() tea.Msg {
		// Save current state for redo
		redoSHA, err := gitStashCreate()
		if err != nil {
			return UndoDoneMsg{Err: fmt.Errorf("saving redo state: %w", err)}
		}
		if redoSHA != "" {
			ref := fmt.Sprintf("refs/muxd/%s/redo-%d", sessionPrefix, cp.TurnNumber)
			if refErr := gitUpdateRef(ref, redoSHA); refErr != nil {
				return UndoDoneMsg{Err: fmt.Errorf("anchoring redo ref: %w", refErr)}
			}
		}

		if err := gitRestoreClean(); err != nil {
			return UndoDoneMsg{Err: fmt.Errorf("resetting working tree: %w", err)}
		}

		if !cp.IsClean && cp.SHA != "" {
			if err := gitStashApply(cp.SHA); err != nil {
				return UndoDoneMsg{Err: fmt.Errorf("restoring checkpoint: %w", err)}
			}
		}

		return UndoDoneMsg{RestoredTurn: cp.TurnNumber}
	}
}

// RestoreForRedo re-applies a previously undone state.
func RestoreForRedo(cp Checkpoint, sessionPrefix string) tea.Cmd {
	return func() tea.Msg {
		if err := gitRestoreClean(); err != nil {
			return RedoDoneMsg{Err: fmt.Errorf("resetting working tree: %w", err)}
		}

		redoRef := fmt.Sprintf("refs/muxd/%s/redo-%d", sessionPrefix, cp.TurnNumber)
		redoSHA, err := gitRun("rev-parse", redoRef)
		if err != nil {
			return RedoDoneMsg{Err: fmt.Errorf("finding redo ref: %w", err)}
		}

		if err := gitStashApply(redoSHA); err != nil {
			return RedoDoneMsg{Err: fmt.Errorf("applying redo state: %w", err)}
		}

		return RedoDoneMsg{RestoredTurn: cp.TurnNumber}
	}
}

// CleanupCheckpointRefs removes all refs/muxd/<prefix>/* refs for a session.
func CleanupCheckpointRefs(prefix string) tea.Cmd {
	return func() tea.Msg {
		refPrefix := fmt.Sprintf("refs/muxd/%s/", prefix)
		out, err := gitRun("for-each-ref", "--format=%(refname)", refPrefix)
		if err != nil || out == "" {
			return nil
		}
		for _, ref := range strings.Split(out, "\n") {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				if err := gitDeleteRef(ref); err != nil {
					fmt.Fprintf(os.Stderr, "tui: delete git ref %s: %v\n", ref, err)
				}
			}
		}
		return nil
	}
}

func gitStashCreate() (string, error) {
	sha, err := gitRun("stash", "create", "--include-untracked")
	if err != nil {
		return "", err
	}
	return sha, nil
}

func gitUpdateRef(ref, sha string) error {
	_, err := gitRun("update-ref", ref, sha)
	return err
}

func gitDeleteRef(ref string) error {
	_, err := gitRun("update-ref", "-d", ref)
	if err != nil && strings.Contains(err.Error(), "not a valid SHA1") {
		return nil
	}
	return err
}

func gitRestoreClean() error {
	_, err := gitRun("checkout", "--", ".")
	if err != nil && !strings.Contains(err.Error(), "did not match") {
		return err
	}
	_, err = gitRun("clean", "-fd")
	return err
}

func gitStashApply(sha string) error {
	_, err := gitRun("stash", "apply", "--index", sha)
	return err
}

// ---------------------------------------------------------------------------
// Transcript helpers (for clipboard operations)
// ---------------------------------------------------------------------------

func (m Model) lastAssistantMessage() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" {
			return m.messages[i].TextContent()
		}
	}
	return ""
}

func (m Model) plainTranscript() string {
	var b strings.Builder
	for _, msg := range m.messages {
		switch msg.Role {
		case "assistant":
			b.WriteString("Assistant: ")
		case "user":
			b.WriteString("User: ")
		default:
			b.WriteString("System: ")
		}
		b.WriteString(msg.TextContent())
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// Scrollback
// ---------------------------------------------------------------------------

// PrintToScrollback prints rendered text above the active Bubble Tea view.
// This preserves native terminal scrollback and avoids large View reflows.
func PrintToScrollback(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// Add a small visual gap between finalized message blocks.
	return tea.Println(stripTrailingBlankLines(text) + "\n")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// renderError wraps an error message to the terminal width and styles it.
func (m Model) renderError(msg string) string {
	w := m.width
	if w < 20 {
		w = 80
	}
	lines := WrapWords(msg, w-2)
	var styled []string
	for _, l := range lines {
		styled = append(styled, ErrorLineStyle.Render(l))
	}
	return strings.Join(styled, "\n")
}

// stripTrailingBlankLines removes trailing lines that are empty or
// whitespace-only. This prevents triple-blank-line gaps when View()
// appends "\n\n" after each viewLine entry.
func stripTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return ""
	}
	return strings.Join(lines[:end], "\n")
}

// TimeAgo returns a human-readable time-ago string.
func TimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func defaultRuntimeLogPath() string {
	dir, err := config.DataDir()
	if err != nil {
		return ""
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return ""
	}
	return filepath.Join(logDir, "runtime.log")
}

func (m Model) appendRuntimeLog(msg string) {
	if m.runtimeLogPath == "" {
		return
	}
	f, err := os.OpenFile(m.runtimeLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	_, _ = fmt.Fprintf(f, "%s %s\n", ts, msg)
}

func summarizeForLog(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 180 {
		return s[:180] + "..."
	}
	return s
}
