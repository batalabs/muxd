package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"
	"github.com/batalabs/muxd/internal/telegram"
	"github.com/batalabs/muxd/internal/tools"
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
type CompactedMsg struct{}

// AskUserMsg is sent when the agent's ask_user tool needs user input.
type AskUserMsg struct {
	Prompt string
	AskID  string
}

// TitledMsg signals that the session title and tags were generated.
type TitledMsg struct {
	Title string
	Tags  string
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
	toolStatus string

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
	// Tool picker overlay
	toolPicker *ToolPicker
	// Config picker overlay
	configPicker *ConfigPicker

	// MCP tool names (fetched from daemon at startup)
	mcpToolNames []string

	// Rendered message blocks displayed in the View (replaces Prog.Println scrollback)
	viewLines []string

	// Telegram bot state
	telegramCancel context.CancelFunc

	// Runtime diagnostics log path (best effort, may be empty).
	runtimeLogPath string

	// Paste detection: rapid keystrokes (< 5ms apart) indicate pasted text.
	lastKeypressTime time.Time

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
		inputTokens:    session.InputTokens,
		outputTokens:   session.OutputTokens,
		Prefs:          prefs,
		Provider:       prov,
		APIKey:         apiKey,
		runtimeLogPath: defaultRuntimeLogPath(),
	}
	if !resuming {
		m.viewLines = []string{WelcomeStyle.Render("Welcome to muxd. One prompt away from wizardry.")}
	}
	m.appendRuntimeLog("tui initialized")
	return m
}

// Init initializes the Bubble Tea model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, CheckGitRepo()}

	if m.resuming {
		cmds = append(cmds, m.loadSessionHistory())
	}

	// Fetch MCP tool names from daemon in background.
	if m.Daemon != nil {
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
		return m, PrintToScrollback(WelcomeStyle.Render("Context compacted to stay within limits."))

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

	case BranchDoneMsg:
		return m.handleBranchDone(msg)

	case TelegramStartedMsg:
		return m, PrintToScrollback(WelcomeStyle.Render(fmt.Sprintf("Telegram bot @%s started.", msg.BotName)))

	case TelegramErrorMsg:
		m.telegramCancel = nil
		return m, PrintToScrollback(m.renderError("Telegram error: " + msg.Err.Error()))

	case XAuthDoneMsg:
		if msg.Err != nil {
			return m, PrintToScrollback(m.renderError("X auth failed: " + msg.Err.Error()))
		}
		m.Prefs.XAccessToken = msg.Token.AccessToken
		m.Prefs.XRefreshToken = msg.Token.RefreshToken
		m.Prefs.XTokenExpiry = msg.Token.ExpiresAt.UTC().Format(time.RFC3339)
		if err := config.SavePreferences(m.Prefs); err != nil {
			return m, PrintToScrollback(m.renderError("X auth saved token but failed to persist config: " + err.Error()))
		}
		m.applyConfigSetting("x.access_token", m.Prefs.XAccessToken)
		m.applyConfigSetting("x.refresh_token", m.Prefs.XRefreshToken)
		m.applyConfigSetting("x.token_expiry", m.Prefs.XTokenExpiry)
		return m, PrintToScrollback(WelcomeStyle.Render("X auth connected successfully."))

	default:
		return m, nil
	}
}

func (m Model) handleHistoryLoaded() (tea.Model, tea.Cmd) {
	msgs, _ := m.Store.GetMessages(m.Session.ID)
	m.messages = msgs
	m.titled = len(msgs) > 0
	// Populate input history from past user messages (skip tool result messages).
	for _, msg := range msgs {
		if msg.Role == "user" && !msg.HasBlocks() {
			m.history = append(m.history, msg.Content)
		}
	}
	m.resuming = false
	return m, nil
}

func (m Model) handleAskUser(msg AskUserMsg) (tea.Model, tea.Cmd) {
	m.pendingAsk = true
	m.pendingAskID = msg.AskID
	m.thinking = false
	m.toolStatus = ""
	prompt := AsstIconStyle.Render("? ") + msg.Prompt
	m.appendRuntimeLog("ask_user: " + summarizeForLog(msg.Prompt))
	return m, PrintToScrollback(prompt)
}

func (m Model) handleToolStatus(msg ToolStatusMsg) (tea.Model, tea.Cmd) {
	m.toolStatus = "Running " + msg.Name + "..."
	m.appendRuntimeLog("tool_start: " + msg.Name)
	return m, nil
}

func (m Model) handleToolResult(msg ToolResultMsg) (tea.Model, tea.Cmd) {
	m.toolStatus = fmt.Sprintf("Finished %s, waiting for model...", msg.Name)
	m.appendRuntimeLog(fmt.Sprintf("tool_done: %s error=%t bytes=%d", msg.Name, msg.IsError, len(msg.Result)))
	resultFormatted := FormatToolResult(msg.Name, msg.Result, msg.IsError, max(20, m.width-4))
	return m, PrintToScrollback(resultFormatted)
}

func (m Model) handleTurnDone(msg TurnDoneMsg) (tea.Model, tea.Cmd) {
	m.thinking = false
	m.toolStatus = ""
	m.streaming = false
	// streamBuf is already flushed to viewLines by handleStreamDone
	m.streamBuf = ""
	m.streamFlushedLen = 0
	m.appendRuntimeLog("turn_done: " + msg.StopReason)
	return m, nil
}

func (m Model) handleUndoDone(msg UndoDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, PrintToScrollback(m.renderError("Undo failed: " + msg.Err.Error()))
	}
	text := fmt.Sprintf("Undid agent turn %d.", msg.RestoredTurn)
	return m, PrintToScrollback(WelcomeStyle.Render(text))
}

func (m Model) handleRedoDone(msg RedoDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, PrintToScrollback(m.renderError("Redo failed: " + msg.Err.Error()))
	}
	text := fmt.Sprintf("Redid agent turn %d.", msg.RestoredTurn)
	return m, PrintToScrollback(WelcomeStyle.Render(text))
}

// handleShellResult processes the result of a shell command.
func (m Model) handleShellResult(msg ShellResultMsg) (tea.Model, tea.Cmd) {
	m.shellLastOK = msg.Err == nil
	return m, PrintToScrollback(msg.Output)
}

// findBash locates a bash executable. On Windows it checks common Git Bash
// paths before falling back to PATH lookup.
func findBash() string {
	if runtime.GOOS != "windows" {
		return "/bin/sh"
	}
	// Git Bash common locations.
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH lookup.
	if p, err := exec.LookPath("bash.exe"); err == nil {
		return p
	}
	return "cmd.exe"
}

// cmdBuiltins are cmd.exe built-in commands that don't exist as standalone
// executables and must be routed through cmd.exe.
var cmdBuiltins = map[string]bool{
	"dir": true, "type": true, "copy": true, "move": true, "del": true,
	"ren": true, "rename": true, "cls": true, "set": true, "vol": true,
	"ver": true, "color": true, "title": true, "mklink": true, "assoc": true,
	"ftype": true, "pushd": true, "popd": true, "start": true, "erase": true,
}

// shellForCommand picks the right shell and args for a command on Windows.
// Returns (shell, args). On non-Windows, always uses bash.
func shellForCommand(command string) (string, []string) {
	if runtime.GOOS != "windows" {
		return findBash(), []string{"-c", command}
	}

	first := strings.ToLower(strings.Fields(command)[0])

	// PowerShell cmdlets follow Verb-Noun pattern (e.g. Get-Process).
	if strings.Contains(first, "-") && first[0] >= 'a' && first[0] <= 'z' {
		if ps, err := exec.LookPath("pwsh.exe"); err == nil {
			return ps, []string{"-NoProfile", "-Command", command}
		}
		return "powershell.exe", []string{"-NoProfile", "-Command", command}
	}

	// cmd.exe builtins.
	if cmdBuiltins[first] {
		return "cmd.exe", []string{"/c", command}
	}

	// Default to bash for everything else.
	shell := findBash()
	if filepath.Base(shell) == "cmd.exe" {
		return shell, []string{"/c", command}
	}
	return shell, []string{"-c", command}
}

// RunShellCmd runs a shell command in the given directory and returns the
// result via ShellResultMsg.
func RunShellCmd(command, cwd string) tea.Cmd {
	return func() tea.Msg {
		shell, args := shellForCommand(command)
		c := exec.Command(shell, args...)
		c.Dir = cwd
		result, err := c.CombinedOutput()
		output := strings.TrimSpace(string(result))
		if err != nil && output == "" {
			output = "Error: " + err.Error()
		}
		return ShellResultMsg{Output: output, Err: err}
	}
}

// shellGitInfo returns a short git status string for the given directory,
// e.g. "main*" (dirty) or "main" (clean). Returns "" if not a git repo.
func shellGitInfo(cwd string) string {
	branch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branch.Dir = cwd
	out, err := branch.Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))

	dirty := exec.Command("git", "status", "--porcelain")
	dirty.Dir = cwd
	dOut, _ := dirty.Output()
	if len(strings.TrimSpace(string(dOut))) > 0 {
		name += "*"
	}
	return name
}

// View renders the active area at the bottom of the terminal.
func (m Model) View() string {
	var b strings.Builder

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
		if m.streaming {
			// Avoid rendering partial/unclosed markdown in the live preview area.
			// Flushed content still appears in scrollback via flushStreamContent().
			b.WriteString(ThinkingStyle.Render(m.spinner.View()) + "\n\n")
		} else if m.toolStatus != "" {
			b.WriteString(ThinkingStyle.Render(m.spinner.View()+" "+m.toolStatus) + "\n\n")
		} else {
			b.WriteString(ThinkingStyle.Render(m.spinner.View()+" Thinking...") + "\n\n")
		}
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
	footerParts := []string{fmt.Sprintf("muxd %s", m.version)}
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
		tokenStr := fmt.Sprintf("   session tokens: %.1fk", float64(totalTokens)/1000.0)
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
		b.WriteString(FooterMeta.Render(fmt.Sprintf("   cwd: %s", MustGetwd())))
	}
	if m.Prefs.FooterSession {
		b.WriteString("\n")
		sessionLine := fmt.Sprintf("   session: %s", m.Session.ID[:8])
		if m.Session.Title != "New Session" {
			sessionLine = fmt.Sprintf("   session: %s", m.Session.Title)
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
				go m.Daemon.Cancel(m.Session.ID)
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop cancelled."))
		}
		if m.thinking {
			m.thinking = false
			m.streaming = false
			m.toolStatus = ""
			if m.Daemon != nil {
				go m.Daemon.Cancel(m.Session.ID)
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop cancelled."))
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
				go m.Daemon.Cancel(m.Session.ID)
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop cancelled."))
		}
		if m.thinking {
			m.thinking = false
			m.streaming = false
			m.toolStatus = ""
			if m.Daemon != nil {
				go m.Daemon.Cancel(m.Session.ID)
			}
			return m, PrintToScrollback(WelcomeStyle.Render("Agent loop cancelled."))
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
		// indicate pasted text — treat Enter as a literal newline, not submit.
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

// handlePickerKey intercepts all keys when the session picker is active.
func (m Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.picker.Mode() {
	case pickerRenaming:
		return m.handlePickerRename(msg)
	case pickerConfirmDelete:
		return m.handlePickerDelete(msg)
	default:
		return m.handlePickerBrowse(msg)
	}
}

func (m Model) handlePickerBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		if m.picker.SelectedCount() > 0 {
			m.picker.ClearSelected()
			return m, nil
		}
		m.picker.Dismiss()
		m.picker = nil
		return m, nil

	case tea.KeyEnter:
		if m.picker.SelectedCount() >= 2 {
			return m, nil
		}
		sel := m.picker.SelectedSession()
		if sel == nil {
			return m, nil
		}
		sess := *sel
		m.picker.Dismiss()
		m.picker = nil
		// Switch to selected session
		m.Session = &sess
		m.messages = nil
		m.inputTokens = sess.InputTokens
		m.outputTokens = sess.OutputTokens
		m.lastInputTokens = 0
		m.lastOutputTokens = 0
		m.cacheCreationInputTokens = 0
		m.cacheReadInputTokens = 0
		m.lastCacheCreationInputTokens = 0
		m.lastCacheReadInputTokens = 0
		m.titled = true
		m.history = nil
		m.historyIdx = -1
		m.historyDraft = ""
		m.resuming = true
		return m, m.loadSessionHistory()

	case tea.KeyUp:
		m.picker.MoveUp()
		return m, nil

	case tea.KeyDown:
		m.picker.MoveDown()
		return m, nil

	case tea.KeyBackspace, tea.KeyDelete:
		m.picker.BackspaceFilter()
		return m, nil

	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			// Space always toggles selection (never added to filter).
			if len(msg.Runes) == 1 && msg.Runes[0] == ' ' {
				m.picker.ToggleSelected()
				m.picker.MoveDown()
				return m, nil
			}
			for _, r := range msg.Runes {
				// 'r' starts rename, 'd' starts delete (only when filter is empty)
				if len(msg.Runes) == 1 && m.picker.filter == "" {
					switch msg.Runes[0] {
					case 'r':
						if m.picker.SelectedCount() <= 1 {
							m.picker.StartRename()
							return m, nil
						}
					case 'd':
						m.picker.StartDelete()
						return m, nil
					case 'a':
						m.picker.SelectAll()
						return m, nil
					}
				}
				m.picker.AppendFilter(r)
			}
		}
		return m, nil
	}
}

func (m Model) handlePickerRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.picker.CancelMode()
		return m, nil
	case tea.KeyEnter:
		id, newTitle := m.picker.CommitRename()
		if id != "" && newTitle != "" {
			if m.Store != nil {
				if err := m.Store.UpdateSessionTitle(id, newTitle); err != nil {
					fmt.Fprintf(os.Stderr, "tui: update session title: %v\n", err)
				}
			}
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		m.picker.BackspaceRename()
		return m, nil
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				m.picker.AppendRename(r)
			}
		}
		return m, nil
	}
}

func (m Model) handlePickerDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.picker.CancelMode()
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'y', 'Y':
				if m.picker.SelectedCount() > 0 {
					ids := m.picker.RemoveSelectedMulti()
					if m.Store != nil {
						for _, id := range ids {
							if err := m.Store.DeleteSession(id); err != nil {
								fmt.Fprintf(os.Stderr, "tui: delete session %s: %v\n", id[:8], err)
							}
						}
					}
				} else {
					id := m.picker.RemoveSelected()
					if id != "" && m.Store != nil {
						if err := m.Store.DeleteSession(id); err != nil {
							fmt.Fprintf(os.Stderr, "tui: delete session: %v\n", err)
						}
					}
				}
				return m, nil
			case 'n', 'N':
				m.picker.CancelMode()
				return m, nil
			}
		}
	}
	return m, nil
}

// openSessionPicker fetches sessions and opens the picker.
func (m Model) openSessionPicker() tea.Cmd {
	daemon := m.Daemon
	store := m.Store
	return func() tea.Msg {
		var sessions []domain.Session
		var err error
		if daemon != nil {
			sessions, err = daemon.ListSessions("", 100)
		} else if store != nil {
			sessions, err = store.ListSessions("", 100)
		}
		return SessionPickerMsg{Sessions: sessions, Err: err}
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

// submit sends the user's message or handles a slash command.
func (m Model) submit(trimmed string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(trimmed, "/") {
		return m.handleSlashCommand(trimmed)
	}

	// Require model selection before chatting
	if m.Prefs.Model == "" {
		return m, PrintToScrollback(m.renderError("No model selected. Use /config set model <name> (e.g. /config set model claude-sonnet)"))
	}

	// Check for API key before sending to the daemon
	if m.APIKey == "" {
		provName := ""
		if m.Provider != nil {
			provName = m.Provider.Name()
		}
		if provName != "ollama" {
			if provName == "" {
				provName = "your_provider"
			}
			hint := fmt.Sprintf("No API key set. Use /config set %s.api_key <key>", provName)
			return m, PrintToScrollback(m.renderError(hint))
		}
	}

	userMsg := domain.TranscriptMessage{Role: "user", Content: trimmed}
	m.messages = append(m.messages, userMsg)
	m.history = append(m.history, trimmed)
	m.historyIdx = -1
	m.historyDraft = ""
	m.setInput("")
	m.thinking = true
	m.streaming = false
	m.streamBuf = ""
	m.streamFlushedLen = 0
	m.appendRuntimeLog("submit: " + summarizeForLog(trimmed))

	formatted := FormatMessageForScrollback(userMsg, m.width)
	cmds := []tea.Cmd{
		PrintToScrollback(formatted),
		StreamViaDaemon(m.Daemon, m.Session.ID, trimmed),
		m.spinner.Tick,
	}
	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Slash commands
// ---------------------------------------------------------------------------

func (m Model) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	m.setInput("")

	clean := strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(input))

	parts := strings.Fields(clean)
	if len(parts) == 0 {
		return m, nil
	}
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/clear":
		m.messages = nil
		m.inputTokens = 0
		m.outputTokens = 0
		m.lastInputTokens = 0
		m.lastOutputTokens = 0
		m.cacheCreationInputTokens = 0
		m.cacheReadInputTokens = 0
		m.lastCacheCreationInputTokens = 0
		m.lastCacheReadInputTokens = 0
		return m, tea.ClearScreen

	case "/exit", "/quit":
		return m, tea.Quit

	case "/new":
		cwd := MustGetwd()
		oldPrefix := m.Session.ID[:8]
		if m.Daemon != nil {
			sessionID, err := m.Daemon.CreateSession(cwd, m.modelID)
			if err != nil {
				return m, PrintToScrollback(m.renderError("Failed to create session: " + err.Error()))
			}
			sess, err := m.Daemon.GetSession(sessionID)
			if err != nil {
				return m, PrintToScrollback(m.renderError("Failed to get session: " + err.Error()))
			}
			m.Session = sess
		} else {
			sess, err := m.Store.CreateSession(cwd, m.modelID)
			if err != nil {
				return m, PrintToScrollback(m.renderError("Failed to create session: " + err.Error()))
			}
			m.Session = sess
		}
		m.messages = nil
		m.inputTokens = 0
		m.outputTokens = 0
		m.lastInputTokens = 0
		m.lastOutputTokens = 0
		m.cacheCreationInputTokens = 0
		m.cacheReadInputTokens = 0
		m.lastCacheCreationInputTokens = 0
		m.lastCacheReadInputTokens = 0
		m.titled = false
		m.history = nil
		m.historyIdx = -1
		m.historyDraft = ""
		m.checkpoints = nil
		m.redoStack = nil
		var cmds []tea.Cmd
		cmds = append(cmds, PrintToScrollback(WelcomeStyle.Render("New session started.")))
		if m.gitAvailable {
			cmds = append(cmds, CleanupCheckpointRefs(oldPrefix))
		}
		return m, tea.Batch(cmds...)

	case "/refresh":
		return m.refreshCurrentSession()

	case "/branch":
		if m.thinking {
			return m, PrintToScrollback(m.renderError("Cannot branch while agent is running."))
		}
		atSequence := 0
		if len(parts) >= 3 && parts[1] == "--at" {
			if n, err := strconv.Atoi(parts[2]); err == nil && n > 0 {
				atSequence = n
			} else {
				return m, PrintToScrollback(m.renderError("Usage: /branch [--at N]"))
			}
		}
		sessionID := m.Session.ID
		daemon := m.Daemon
		store := m.Store
		return m, func() tea.Msg {
			if daemon != nil {
				sess, err := daemon.BranchSession(sessionID, atSequence)
				return BranchDoneMsg{Session: sess, Err: err}
			}
			if store != nil {
				sess, err := store.BranchSession(sessionID, atSequence)
				return BranchDoneMsg{Session: sess, Err: err}
			}
			return BranchDoneMsg{Err: fmt.Errorf("no store available")}
		}

	case "/rename":
		if len(parts) < 2 {
			return m, PrintToScrollback(m.renderError("Usage: /rename <new title>"))
		}
		newTitle := strings.Join(parts[1:], " ")
		if m.Store != nil {
			if err := m.Store.UpdateSessionTitle(m.Session.ID, newTitle); err != nil {
				fmt.Fprintf(os.Stderr, "tui: update session title: %v\n", err)
			}
		}
		m.Session.Title = newTitle
		m.titled = true
		return m, PrintToScrollback(WelcomeStyle.Render(fmt.Sprintf("Session renamed to: %s", newTitle)))

	case "/sessions":
		return m, m.openSessionPicker()

	case "/continue", "/resume":
		if len(parts) < 2 {
			return m.handleSlashCommand("/sessions")
		}
		targetID := parts[1]
		var sess *domain.Session
		var err error
		if m.Daemon != nil {
			sess, err = m.Daemon.GetSession(targetID)
		} else {
			sess, err = m.Store.FindSessionByPrefix(targetID)
		}
		if err != nil {
			return m, PrintToScrollback(m.renderError("Session not found: " + targetID))
		}
		m.Session = sess
		m.messages = nil
		m.inputTokens = sess.InputTokens
		m.outputTokens = sess.OutputTokens
		m.lastInputTokens = 0
		m.lastOutputTokens = 0
		m.cacheCreationInputTokens = 0
		m.cacheReadInputTokens = 0
		m.lastCacheCreationInputTokens = 0
		m.lastCacheReadInputTokens = 0
		m.titled = true
		m.history = nil
		m.historyIdx = -1
		m.historyDraft = ""
		m.resuming = true
		return m, m.loadSessionHistory()

	case "/config":
		if len(parts) == 1 {
			m.configPicker = NewConfigPicker(m.Prefs)
			return m, nil
		}
		if len(parts) == 2 {
			sub := strings.ToLower(strings.TrimSpace(parts[1]))
			switch sub {
			case "models", "tools", "messaging", "theme":
				m.configPicker = NewConfigPickerAtGroup(m.Prefs, sub)
				return m, nil
			}
		}
		result, err := config.ExecuteConfigAction(&m.Prefs, parts[1:])
		if err != nil {
			return m, PrintToScrollback(m.renderError(err.Error()))
		}
		// After a successful "set", propagate runtime changes
		if len(parts) >= 4 && strings.ToLower(parts[1]) == "set" {
			key := parts[2]
			m.applyConfigSetting(key, parts[3])
		}
		var styled []string
		for _, line := range strings.Split(result, "\n") {
			styled = append(styled, FooterMeta.Render(line))
		}
		return m, PrintToScrollback(strings.Join(styled, "\n"))

	case "/undo":
		if !m.gitAvailable {
			return m, PrintToScrollback(m.renderError("Undo requires a git repository."))
		}
		if m.thinking {
			return m, PrintToScrollback(m.renderError("Cannot undo while agent is running."))
		}
		if len(m.checkpoints) == 0 {
			return m, PrintToScrollback(m.renderError("Nothing to undo."))
		}
		cp := m.checkpoints[len(m.checkpoints)-1]
		m.checkpoints = m.checkpoints[:len(m.checkpoints)-1]
		m.redoStack = append(m.redoStack, cp)
		sessionPrefix := m.Session.ID[:8]
		return m, RestoreCheckpoint(cp, sessionPrefix)

	case "/redo":
		if !m.gitAvailable {
			return m, PrintToScrollback(m.renderError("Redo requires a git repository."))
		}
		if m.thinking {
			return m, PrintToScrollback(m.renderError("Cannot redo while agent is running."))
		}
		if len(m.redoStack) == 0 {
			return m, PrintToScrollback(m.renderError("Nothing to redo."))
		}
		cp := m.redoStack[len(m.redoStack)-1]
		m.redoStack = m.redoStack[:len(m.redoStack)-1]
		m.checkpoints = append(m.checkpoints, cp)
		sessionPrefix := m.Session.ID[:8]
		return m, RestoreForRedo(cp, sessionPrefix)

	case "/sh":
		m.shellActive = true
		m.shellInput = ""
		m.shellInputCursor = 0
		m.shellLastOK = true
		// Get current working directory for shell prompt
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "~"
		}
		m.shellCwd = cwd
		return m, PrintToScrollback(WelcomeStyle.Render("Entered muxd shell. Type commands directly. Use 'exit' to return."))

	case "/telegram":
		return m.handleTelegram(parts[1:])

	case "/qr":
		return m.handleQRCommand(parts[1:])

	case "/remember":
		return m.handleRememberCommand(parts[1:])

	case "/tools":
		return m.handleToolsCommand(parts[1:])

	case "/tweet":
		return m.handleTweetCommand(parts[1:])
	case "/schedule":
		return m.handleScheduleCommand(parts[1:])
	case "/x":
		return m.handleXCommand(parts[1:])

	case "/help":
		cmds := domain.CommandHelp(false)
		grouped := make(map[string][]domain.CommandDef)
		for _, c := range cmds {
			grouped[c.Group] = append(grouped[c.Group], c)
		}
		var lines []string
		for _, g := range domain.CommandGroups {
			defs := grouped[g.Key]
			if len(defs) == 0 {
				continue
			}
			lines = append(lines, FooterHead.Render(g.Label))
			for _, c := range defs {
				lines = append(lines, FooterMeta.Render(fmt.Sprintf("  %-18s %s", c.Name, c.Description)))
			}
			lines = append(lines, "")
		}
		lines = append(lines, FooterMeta.Render("  Ctrl+R to open session picker  |  Tab to autocomplete"))
		return m, PrintToScrollback(strings.Join(lines, "\n"))

	default:
		return m, PrintToScrollback(m.renderError("Unknown command: " + cmd + "  (try /help)"))
	}
}

func (m Model) handleRememberCommand(args []string) (tea.Model, tea.Cmd) {
	cwd, _ := tools.Getwd()
	if cwd == "" {
		return m, PrintToScrollback(m.renderError("Cannot determine working directory."))
	}

	mem := tools.NewProjectMemory(cwd)

	// /remember --remove <key>
	if len(args) >= 2 && args[0] == "--remove" {
		key := args[1]
		facts, err := mem.Load()
		if err != nil {
			return m, PrintToScrollback(m.renderError("Loading memory: " + err.Error()))
		}
		if _, ok := facts[key]; !ok {
			return m, PrintToScrollback(m.renderError("Key " + key + " not found in project memory."))
		}
		delete(facts, key)
		if err := mem.Save(facts); err != nil {
			return m, PrintToScrollback(m.renderError("Saving memory: " + err.Error()))
		}
		return m, PrintToScrollback(WelcomeStyle.Render("Removed memory fact: " + key))
	}

	// /remember <key> <value...>
	if len(args) < 2 {
		// Show current facts
		facts, err := mem.Load()
		if err != nil {
			return m, PrintToScrollback(m.renderError("Loading memory: " + err.Error()))
		}
		if len(facts) == 0 {
			return m, PrintToScrollback(FooterMeta.Render("No project memory facts stored. Usage: /remember <key> <value>"))
		}
		formatted := mem.FormatForPrompt()
		var lines []string
		lines = append(lines, FooterHead.Render("Project Memory"))
		for _, line := range strings.Split(formatted, "\n") {
			lines = append(lines, FooterMeta.Render("  "+line))
		}
		return m, PrintToScrollback(strings.Join(lines, "\n"))
	}

	key := args[0]
	value := strings.Join(args[1:], " ")

	facts, err := mem.Load()
	if err != nil {
		return m, PrintToScrollback(m.renderError("Loading memory: " + err.Error()))
	}
	facts[key] = value
	if err := mem.Save(facts); err != nil {
		return m, PrintToScrollback(m.renderError("Saving memory: " + err.Error()))
	}

	return m, PrintToScrollback(WelcomeStyle.Render(fmt.Sprintf("Saved memory fact: %s = %s", key, value)))
}

func (m Model) handleToolsCommand(args []string) (tea.Model, tea.Cmd) {
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}
	disabled := m.Prefs.DisabledToolsSet()
	toolNames := tools.ToolNames()
	// Merge MCP tool names into the list.
	if len(m.mcpToolNames) > 0 {
		toolNames = append(toolNames, m.mcpToolNames...)
		sort.Strings(toolNames)
	}

	switch sub {
	case "list":
		m.toolPicker = NewToolPicker(toolNames, disabled)
		return m, nil

	case "profile":
		if len(args) < 2 {
			return m, PrintToScrollback(m.renderError("Usage: /tools profile <safe|coder|research>"))
		}
		profile := strings.ToLower(strings.TrimSpace(args[1]))
		if profile != "safe" && profile != "coder" && profile != "research" {
			return m, PrintToScrollback(m.renderError("Unknown profile: " + profile))
		}
		disabled = tools.ToolProfileDisabledSet(profile)
		m.applyDisabledToolsSetting(disabled)
		return m, PrintToScrollback(WelcomeStyle.Render("Applied tools profile: " + profile))

	case "enable", "disable", "toggle":
		if len(args) < 2 {
			return m, PrintToScrollback(m.renderError("Usage: /tools " + sub + " <tool_name>"))
		}
		name := tools.NormalizeToolName(args[1])
		// Accept both built-in tools and MCP tools.
		_, isBuiltin := tools.FindTool(name)
		isMCP := mcp.IsMCPTool(name)
		if !isBuiltin && !isMCP {
			return m, PrintToScrollback(m.renderError("Unknown tool: " + name))
		}
		switch sub {
		case "enable":
			delete(disabled, name)
		case "disable":
			disabled[name] = true
		case "toggle":
			if disabled[name] {
				delete(disabled, name)
			} else {
				disabled[name] = true
			}
		}

		m.applyDisabledToolsSetting(disabled)
		status := "enabled"
		if disabled[name] {
			status = "disabled"
		}
		return m, PrintToScrollback(WelcomeStyle.Render(fmt.Sprintf("Tool %s is now %s.", name, status)))

	default:
		return m, PrintToScrollback(m.renderError("Usage: /tools [list|enable <name>|disable <name>|toggle <name>|profile <safe|coder|research>]"))
	}
}

func (m Model) handleTweetCommand(args []string) (tea.Model, tea.Cmd) {
	if m.Store == nil {
		return m, PrintToScrollback(m.renderError("Tweet scheduler unavailable: no store configured."))
	}
	if len(args) == 0 {
		return m, PrintToScrollback(m.renderError("Usage: /tweet <text> | /tweet --schedule <HH:MM|RFC3339> [--daily|--hourly] <text> | /tweet --list | /tweet --cancel <id>"))
	}

	if args[0] == "--list" {
		items, err := m.Store.ListScheduledToolJobs(100)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Failed to list scheduled tweets: " + err.Error()))
		}
		var xJobs []store.ScheduledToolJob
		for _, it := range items {
			if it.ToolName == "x_post" {
				xJobs = append(xJobs, it)
			}
		}
		if len(xJobs) == 0 {
			return m, PrintToScrollback(WelcomeStyle.Render("No scheduled tweets."))
		}
		var lines []string
		lines = append(lines, FooterHead.Render("Scheduled tweets"))
		for _, it := range xJobs {
			id := it.ID
			if len(id) > 8 {
				id = id[:8]
			}
			text := ""
			if v, ok := it.ToolInput["text"].(string); ok {
				text = v
			}
			line := fmt.Sprintf("  %-8s %-9s %-9s %s", id, it.ScheduledFor.Local().Format("2006-01-02 15:04"), it.Status, summarizeForLog(text))
			lines = append(lines, FooterMeta.Render(line))
		}
		return m, PrintToScrollback(strings.Join(lines, "\n"))
	}

	if args[0] == "--cancel" {
		if len(args) < 2 {
			return m, PrintToScrollback(m.renderError("Usage: /tweet --cancel <id>"))
		}
		if err := m.Store.CancelScheduledToolJob(args[1]); err != nil {
			return m, PrintToScrollback(m.renderError("Failed to cancel scheduled tweet: " + err.Error()))
		}
		return m, PrintToScrollback(WelcomeStyle.Render("Cancelled scheduled tweet: " + args[1]))
	}

	recurrence := "once"
	scheduleRaw := ""
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "--") {
		switch args[i] {
		case "--schedule":
			if i+1 >= len(args) {
				return m, PrintToScrollback(m.renderError("Usage: /tweet --schedule <HH:MM|RFC3339> [--daily|--hourly] <text>"))
			}
			scheduleRaw = args[i+1]
			i += 2
		case "--daily":
			recurrence = "daily"
			i++
		case "--hourly":
			recurrence = "hourly"
			i++
		default:
			return m, PrintToScrollback(m.renderError("Unknown flag: " + args[i]))
		}
	}

	text := strings.TrimSpace(strings.Join(args[i:], " "))
	if text == "" {
		return m, PrintToScrollback(m.renderError("Tweet text cannot be empty."))
	}

	if scheduleRaw == "" {
		token, refreshed, err := tools.ResolveXPostTokenFromPrefs(&m.Prefs)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Tweet failed: " + err.Error()))
		}
		if refreshed {
			if saveErr := config.SavePreferences(m.Prefs); saveErr == nil {
				m.applyConfigSetting("x.access_token", m.Prefs.XAccessToken)
				m.applyConfigSetting("x.refresh_token", m.Prefs.XRefreshToken)
				m.applyConfigSetting("x.token_expiry", m.Prefs.XTokenExpiry)
			}
		}
		id, url, err := tools.PostXTweet(text, token)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Tweet failed: " + err.Error()))
		}
		return m, PrintToScrollback(WelcomeStyle.Render(fmt.Sprintf("Posted tweet %s\n%s", id, url)))
	}

	scheduledFor, err := tools.ParseTweetScheduleTime(scheduleRaw, time.Now())
	if err != nil {
		return m, PrintToScrollback(m.renderError(err.Error()))
	}
	id, err := m.Store.CreateScheduledToolJob("x_post", map[string]any{"text": text}, scheduledFor, recurrence)
	if err != nil {
		return m, PrintToScrollback(m.renderError("Failed to schedule tweet: " + err.Error()))
	}
	return m, PrintToScrollback(WelcomeStyle.Render(
		fmt.Sprintf("Scheduled tweet %s for %s (%s)", id[:8], scheduledFor.Local().Format("2006-01-02 15:04"), recurrence),
	))
}

func (m Model) handleScheduleCommand(args []string) (tea.Model, tea.Cmd) {
	if m.Store == nil {
		return m, PrintToScrollback(m.renderError("Scheduler unavailable: no store configured."))
	}
	if len(args) == 0 {
		return m, PrintToScrollback(m.renderError("Usage: /schedule add <tool> <HH:MM|RFC3339> <json> [--daily|--hourly] | /schedule add-task <HH:MM|RFC3339> <prompt> [--daily|--hourly] | /schedule list | /schedule cancel <id>"))
	}
	switch strings.ToLower(args[0]) {
	case "list":
		items, err := m.Store.ListScheduledToolJobs(100)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Failed to list scheduled jobs: " + err.Error()))
		}
		if len(items) == 0 {
			return m, PrintToScrollback(WelcomeStyle.Render("No scheduled jobs."))
		}
		var lines []string
		lines = append(lines, FooterHead.Render("Scheduled jobs"))
		for _, it := range items {
			id := it.ID
			if len(id) > 8 {
				id = id[:8]
			}
			displayName := it.ToolName
			if it.ToolName == tools.AgentTaskToolName {
				displayName = "agent_task"
				if p, ok := it.ToolInput["prompt"].(string); ok && p != "" {
					if len(p) > 40 {
						p = p[:40] + "..."
					}
					displayName += ": " + p
				}
			}
			line := fmt.Sprintf("  %-8s %-14s %-9s %s", id, displayName, it.Status, it.ScheduledFor.Local().Format("2006-01-02 15:04"))
			lines = append(lines, FooterMeta.Render(line))
		}
		return m, PrintToScrollback(strings.Join(lines, "\n"))
	case "cancel":
		if len(args) < 2 {
			return m, PrintToScrollback(m.renderError("Usage: /schedule cancel <id>"))
		}
		if err := m.Store.CancelScheduledToolJob(args[1]); err != nil {
			return m, PrintToScrollback(m.renderError("Failed to cancel job: " + err.Error()))
		}
		return m, PrintToScrollback(WelcomeStyle.Render("Cancelled scheduled job: " + args[1]))
	case "add":
		if len(args) < 4 {
			return m, PrintToScrollback(m.renderError("Usage: /schedule add <tool> <HH:MM|RFC3339> <json> [--daily|--hourly]"))
		}
		toolName := tools.NormalizeToolName(args[1])
		if _, ok := tools.FindTool(toolName); !ok {
			return m, PrintToScrollback(m.renderError("Unknown tool: " + toolName))
		}
		scheduledFor, err := tools.ParseTweetScheduleTime(args[2], time.Now())
		if err != nil {
			return m, PrintToScrollback(m.renderError(err.Error()))
		}
		recurrence := "once"
		rawTail := strings.TrimSpace(strings.Join(args[3:], " "))
		if strings.HasSuffix(rawTail, " --daily") {
			recurrence = "daily"
			rawTail = strings.TrimSpace(strings.TrimSuffix(rawTail, " --daily"))
		} else if strings.HasSuffix(rawTail, " --hourly") {
			recurrence = "hourly"
			rawTail = strings.TrimSpace(strings.TrimSuffix(rawTail, " --hourly"))
		}
		var input map[string]any
		if err := json.Unmarshal([]byte(rawTail), &input); err != nil {
			return m, PrintToScrollback(m.renderError("Invalid JSON tool input: " + err.Error()))
		}
		id, err := m.Store.CreateScheduledToolJob(toolName, input, scheduledFor, recurrence)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Failed to schedule job: " + err.Error()))
		}
		return m, PrintToScrollback(WelcomeStyle.Render(
			fmt.Sprintf("Scheduled job %s: %s at %s (%s)", id[:8], toolName, scheduledFor.Local().Format("2006-01-02 15:04"), recurrence),
		))
	case "add-task":
		if len(args) < 3 {
			return m, PrintToScrollback(m.renderError("Usage: /schedule add-task <HH:MM|RFC3339> <prompt> [--daily|--hourly]"))
		}
		scheduledFor, err := tools.ParseTweetScheduleTime(args[1], time.Now())
		if err != nil {
			return m, PrintToScrollback(m.renderError(err.Error()))
		}
		recurrence := "once"
		rawTail := strings.TrimSpace(strings.Join(args[2:], " "))
		if strings.HasSuffix(rawTail, " --daily") {
			recurrence = "daily"
			rawTail = strings.TrimSpace(strings.TrimSuffix(rawTail, " --daily"))
		} else if strings.HasSuffix(rawTail, " --hourly") {
			recurrence = "hourly"
			rawTail = strings.TrimSpace(strings.TrimSuffix(rawTail, " --hourly"))
		}
		if strings.TrimSpace(rawTail) == "" {
			return m, PrintToScrollback(m.renderError("prompt is required"))
		}
		input := map[string]any{"prompt": rawTail}
		id, err := m.Store.CreateScheduledToolJob(tools.AgentTaskToolName, input, scheduledFor, recurrence)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Failed to schedule task: " + err.Error()))
		}
		return m, PrintToScrollback(WelcomeStyle.Render(
			fmt.Sprintf("Scheduled agent task %s at %s (%s)", id[:8], scheduledFor.Local().Format("2006-01-02 15:04"), recurrence),
		))
	default:
		return m, PrintToScrollback(m.renderError("Usage: /schedule [add|add-task|list|cancel]"))
	}
}

func (m Model) handleXCommand(args []string) (tea.Model, tea.Cmd) {
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch sub {
	case "status":
		expiry := strings.TrimSpace(m.Prefs.XTokenExpiry)
		if strings.TrimSpace(m.Prefs.XAccessToken) == "" {
			return m, PrintToScrollback(WelcomeStyle.Render("X auth: not connected. Run /x auth"))
		}
		msg := "X auth: connected"
		if expiry != "" {
			msg += " (expires " + expiry + ")"
		}
		return m, PrintToScrollback(WelcomeStyle.Render(msg))
	case "logout":
		m.Prefs.XAccessToken = ""
		m.Prefs.XRefreshToken = ""
		m.Prefs.XTokenExpiry = ""
		if err := config.SavePreferences(m.Prefs); err != nil {
			return m, PrintToScrollback(m.renderError("Failed to save config: " + err.Error()))
		}
		m.applyConfigSetting("x.access_token", "")
		m.applyConfigSetting("x.refresh_token", "")
		m.applyConfigSetting("x.token_expiry", "")
		return m, PrintToScrollback(WelcomeStyle.Render("X auth cleared."))
	case "auth":
		scopes := []string{"tweet.read", "tweet.write", "users.read", "offline.access"}
		authURL, waitCode, closeFn, err := tools.StartXOAuthLocal(m.Prefs.XClientID, scopes, m.Prefs.XRedirectURL)
		if err != nil {
			return m, PrintToScrollback(m.renderError("X auth setup failed: " + err.Error()))
		}
		if err := tools.OpenBrowser(authURL); err != nil {
			fmt.Fprintf(os.Stderr, "tui: open browser: %v\n", err)
		}
		cmd := func() tea.Msg {
			defer closeFn()
			payload, waitErr := waitCode(5 * time.Minute)
			if waitErr != nil {
				return XAuthDoneMsg{Err: waitErr}
			}
			parts := strings.SplitN(payload, "|", 3)
			if len(parts) != 3 {
				return XAuthDoneMsg{Err: fmt.Errorf("invalid callback payload")}
			}
			code, redirectURL, verifier := parts[0], parts[1], parts[2]
			tok, exchErr := tools.ExchangeXOAuthCode(
				m.Prefs.XClientID,
				m.Prefs.XClientSecret,
				code,
				redirectURL,
				verifier,
			)
			if exchErr != nil {
				return XAuthDoneMsg{Err: exchErr}
			}
			return XAuthDoneMsg{Token: tok}
		}
		return m, tea.Batch(
			PrintToScrollback(WelcomeStyle.Render("Opening browser for X auth...")),
			PrintToScrollback(FooterMeta.Render("If browser did not open, visit:\n"+authURL)),
			cmd,
		)
	default:
		return m, PrintToScrollback(m.renderError("Usage: /x [auth|status|logout]"))
	}
}

func (m Model) handleToolPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.toolPicker.Dismiss()
		m.toolPicker = nil
		return m, nil
	case tea.KeyUp:
		m.toolPicker.MoveUp()
		return m, nil
	case tea.KeyDown:
		m.toolPicker.MoveDown()
		return m, nil
	case tea.KeyEnter, tea.KeySpace:
		name := m.toolPicker.SelectedName()
		if name == "" {
			return m, nil
		}
		m.toolPicker.ToggleSelected()
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'a', 'A':
				m.applyDisabledToolsSetting(m.toolPicker.DisabledMap())
				m.toolPicker.MarkApplied()
				return m, PrintToScrollback(WelcomeStyle.Render("Applied tool changes."))
			case 'c', 'C':
				m.toolPicker.ResetToBaseline()
				m.toolPicker.Dismiss()
				m.toolPicker = nil
				return m, PrintToScrollback(WelcomeStyle.Render("Cancelled tool changes."))
			case 'p', 'P':
				// Cycle: safe -> coder -> research -> safe
				cur := m.toolPicker.DisabledMap()
				next := "safe"
				if mapsEqualBool(cur, tools.ToolProfileDisabledSet("safe")) {
					next = "coder"
				} else if mapsEqualBool(cur, tools.ToolProfileDisabledSet("coder")) {
					next = "research"
				}
				allNames := tools.ToolNames()
				if len(m.mcpToolNames) > 0 {
					allNames = append(allNames, m.mcpToolNames...)
					sort.Strings(allNames)
				}
				m.toolPicker = NewToolPicker(allNames, tools.ToolProfileDisabledSet(next))
				return m, PrintToScrollback(WelcomeStyle.Render("Staged tools profile: " + next + " (press 'a' to apply)"))
			}
		}
		if len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				if r >= 32 {
					m.toolPicker.AppendFilter(r)
				}
			}
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		m.toolPicker.BackspaceFilter()
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) applyDisabledToolsSetting(disabled map[string]bool) {
	disabledCSV := disabledToolsCSV(disabled)
	m.Prefs.ToolsDisabled = disabledCSV
	if m.Daemon != nil {
		if _, err := m.Daemon.SetConfig("tools.disabled", disabledCSV); err != nil {
			fmt.Fprintf(os.Stderr, "tui: set disabled tools config: %v\n", err)
		}
	} else {
		if err := config.SavePreferences(m.Prefs); err != nil {
			fmt.Fprintf(os.Stderr, "tui: save preferences: %v\n", err)
		}
	}
}

func mapsEqualBool(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

func disabledToolsCSV(disabled map[string]bool) string {
	var out []string
	for name, off := range disabled {
		if off {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return ""
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func (m *Model) applyConfigSetting(key, value string) {
	// API key changed — update daemon and local state
	if strings.HasSuffix(key, ".api_key") {
		if m.Daemon != nil {
			if _, err := m.Daemon.SetConfig(key, value); err != nil {
				fmt.Fprintf(os.Stderr, "tui: set config %s: %v\n", key, err)
			}
		}
		provName := strings.TrimSuffix(key, ".api_key")
		if resolved, rerr := config.LoadProviderAPIKey(m.Prefs, provName); rerr == nil {
			m.APIKey = resolved
		}
	}
	// Model changed — resolve provider, update runtime
	if key == "model" {
		name := value
		currentProvName := ""
		if m.Provider != nil {
			currentProvName = m.Provider.Name()
		}
		newProvName, newID := provider.ResolveProviderAndModel(name, currentProvName)
		if newProvName != currentProvName {
			if newProv, provErr := provider.GetProvider(newProvName); provErr == nil {
				m.Provider = newProv
			}
			if newKey, keyErr := config.LoadProviderAPIKey(m.Prefs, newProvName); keyErr == nil {
				m.APIKey = newKey
			}
		}
		m.modelID = newID
		m.modelLabel = name
		if m.Daemon != nil {
			if err := m.Daemon.SetModel(m.Session.ID, name, newID); err != nil {
				fmt.Fprintf(os.Stderr, "tui: set model: %v\n", err)
			}
		}
	}
	if key == "ollama.url" {
		provider.SetOllamaBaseURL(value)
		if m.Daemon != nil {
			if _, err := m.Daemon.SetConfig(key, value); err != nil {
				fmt.Fprintf(os.Stderr, "tui: set config %s: %v\n", key, err)
			}
		}
	}
	if strings.HasPrefix(key, "x.") && m.Daemon != nil {
		if _, err := m.Daemon.SetConfig(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "tui: set config %s: %v\n", key, err)
		}
	}
	if key == "scheduler.allowed_tools" && m.Daemon != nil {
		if _, err := m.Daemon.SetConfig(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "tui: set config %s: %v\n", key, err)
		}
	}
	if key == "tools.disabled" && m.Daemon != nil {
		if _, err := m.Daemon.SetConfig(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "tui: set config %s: %v\n", key, err)
		}
	}
}

func (m Model) validateConfigInput(key, value string) error {
	value = config.SanitizeValue(value)
	switch key {
	case "model":
		currentProvName := ""
		if m.Provider != nil {
			currentProvName = m.Provider.Name()
		}
		resolvedProv, modelID := provider.ResolveProviderAndModel(value, currentProvName)
		if strings.TrimSpace(modelID) == "" {
			return fmt.Errorf("model cannot be empty")
		}
		// Warn if the resolved provider has no API key — the user likely
		// intended a different provider but typed a bare model name.
		if resolvedProv != "ollama" && !strings.Contains(value, "/") {
			if _, keyErr := config.LoadProviderAPIKey(m.Prefs, resolvedProv); keyErr != nil {
				return fmt.Errorf("model %q resolved to %s but no API key is set; use %s/%s to specify provider explicitly",
					value, resolvedProv, resolvedProv, value)
			}
		}
	case "telegram.allowed_ids":
		if _, err := config.ParseAllowedIDs(value); err != nil {
			return err
		}
	case "tools.disabled":
		disabled := config.Preferences{ToolsDisabled: value}.DisabledToolsSet()
		for name := range disabled {
			if _, ok := tools.FindTool(name); !ok {
				return fmt.Errorf("unknown tool: %s", name)
			}
		}
	case "scheduler.allowed_tools":
		allowed := config.Preferences{SchedulerAllowedTools: value}.ScheduledAllowedToolsSet()
		for name := range allowed {
			if _, ok := tools.FindTool(name); !ok {
				return fmt.Errorf("unknown tool: %s", name)
			}
		}
	}
	return nil
}

func isBoolConfigKey(key string) bool {
	switch key {
	case "footer.tokens", "footer.cost", "footer.cwd", "footer.session", "footer.keybindings":
		return true
	default:
		return false
	}
}

func (m Model) configEditInitialValue(key string) string {
	switch key {
	case "anthropic.api_key", "zai.api_key", "grok.api_key", "mistral.api_key", "openai.api_key", "google.api_key", "brave.api_key", "fireworks.api_key", "x.client_secret", "x.access_token", "x.refresh_token", "telegram.bot_token":
		return ""
	default:
		return m.Prefs.Get(key)
	}
}

func (m Model) handleConfigPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		if m.configPicker.mode == configPickerGroups {
			m.configPicker.Dismiss()
			m.configPicker = nil
			return m, nil
		}
		m.configPicker.Back()
		return m, nil
	case tea.KeyUp:
		m.configPicker.MoveUp()
		return m, nil
	case tea.KeyDown:
		m.configPicker.MoveDown()
		return m, nil
	case tea.KeyEnter:
		switch m.configPicker.mode {
		case configPickerGroups:
			m.configPicker.EnterGroup()
			return m, nil
		case configPickerKeys:
			entry := m.configPicker.selectedEntry()
			if entry == nil {
				return m, nil
			}
			key := entry.Key
			if isBoolConfigKey(key) {
				cur, _ := config.ParseBoolish(m.Prefs.Get(key))
				next := "true"
				if cur {
					next = "false"
				}
				if err := m.Prefs.Set(key, next); err != nil {
					return m, PrintToScrollback(m.renderError("Config update failed: " + err.Error()))
				}
				if err := config.SavePreferences(m.Prefs); err != nil {
					return m, PrintToScrollback(m.renderError("Config save failed: " + err.Error()))
				}
				m.applyConfigSetting(key, next)
				m.configPicker.Refresh(m.Prefs)
				return m, nil
			}
			m.configPicker.StartEdit(key, m.configEditInitialValue(key))
			return m, nil
		case configPickerEdit:
			key, val, ok := m.configPicker.CommitEdit()
			if !ok {
				return m, nil
			}
			if err := m.validateConfigInput(key, val); err != nil {
				return m, PrintToScrollback(m.renderError("Invalid value: " + err.Error()))
			}
			if err := m.Prefs.Set(key, val); err != nil {
				return m, PrintToScrollback(m.renderError("Config update failed: " + err.Error()))
			}
			if err := config.SavePreferences(m.Prefs); err != nil {
				return m, PrintToScrollback(m.renderError("Config save failed: " + err.Error()))
			}
			m.applyConfigSetting(key, val)
			m.configPicker.Refresh(m.Prefs)
			return m, nil
		}
	case tea.KeySpace:
		if m.configPicker.mode == configPickerEdit {
			m.configPicker.AppendEdit(' ')
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if m.configPicker.mode == configPickerEdit {
			m.configPicker.BackspaceEdit()
		}
		return m, nil
	default:
		if m.configPicker.mode == configPickerEdit && msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				m.configPicker.AppendEdit(r)
			}
		}
		return m, nil
	}
	return m, nil
}

// handleShellKey handles key input in shell mode.
func (m Model) handleShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		cmd := strings.TrimSpace(m.shellInput)
		m.shellInput = ""
		m.shellInputCursor = 0
		if cmd == "exit" {
			m.shellActive = false
			return m, PrintToScrollback(WelcomeStyle.Render("Exited muxd shell."))
		}
		if cmd == "/help" {
			return m, PrintToScrollback(shellHelpText())
		}
		if cmd == "" {
			return m, nil
		}
		// Record in history.
		m.shellHistory = append(m.shellHistory, cmd)
		m.shellHistoryIdx = len(m.shellHistory)
		// Handle cd locally so the cwd persists across commands.
		// Match "cd", "cd ", "cd.." (Windows-style), "cd\" etc.
		lower := strings.ToLower(cmd)
		if lower == "cd" || strings.HasPrefix(lower, "cd ") ||
			strings.HasPrefix(lower, "cd..") || strings.HasPrefix(lower, "cd\\") ||
			strings.HasPrefix(lower, "cd/") {
			return m.handleShellCd(cmd)
		}
		// Echo the command before running it.
		echo := FooterMeta.Render("$ " + cmd)
		return m, tea.Batch(PrintToScrollback(echo), RunShellCmd(cmd, m.shellCwd))
	case tea.KeyCtrlC:
		m.shellActive = false
		m.shellInput = ""
		m.shellInputCursor = 0
		return m, PrintToScrollback(WelcomeStyle.Render("Exited muxd shell."))
	case tea.KeyEsc:
		// Esc clears current input; if already empty, exits.
		if m.shellInput != "" {
			m.shellInput = ""
			m.shellInputCursor = 0
			return m, nil
		}
		m.shellActive = false
		return m, PrintToScrollback(WelcomeStyle.Render("Exited muxd shell."))
	case tea.KeyBackspace:
		if m.shellInputCursor > 0 {
			m.shellInput = m.shellInput[:m.shellInputCursor-1] + m.shellInput[m.shellInputCursor:]
			m.shellInputCursor--
		}
		return m, nil
	case tea.KeyLeft:
		if m.shellInputCursor > 0 {
			m.shellInputCursor--
		}
		return m, nil
	case tea.KeyRight:
		if m.shellInputCursor < len(m.shellInput) {
			m.shellInputCursor++
		}
		return m, nil
	case tea.KeyHome, tea.KeyCtrlA:
		m.shellInputCursor = 0
		return m, nil
	case tea.KeyEnd, tea.KeyCtrlE:
		m.shellInputCursor = len(m.shellInput)
		return m, nil
	case tea.KeyUp:
		if len(m.shellHistory) > 0 && m.shellHistoryIdx > 0 {
			m.shellHistoryIdx--
			m.shellInput = m.shellHistory[m.shellHistoryIdx]
			m.shellInputCursor = len(m.shellInput)
		}
		return m, nil
	case tea.KeyDown:
		if m.shellHistoryIdx < len(m.shellHistory)-1 {
			m.shellHistoryIdx++
			m.shellInput = m.shellHistory[m.shellHistoryIdx]
			m.shellInputCursor = len(m.shellInput)
		} else if m.shellHistoryIdx == len(m.shellHistory)-1 {
			m.shellHistoryIdx = len(m.shellHistory)
			m.shellInput = ""
			m.shellInputCursor = 0
		}
		return m, nil
	case tea.KeyTab:
		return m, nil
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				m.shellInput = m.shellInput[:m.shellInputCursor] + string(r) + m.shellInput[m.shellInputCursor:]
				m.shellInputCursor++
			}
		}
		return m, nil
	}
}

// shellHelpText returns a formatted help string for the muxd shell.
func shellHelpText() string {
	var b strings.Builder
	b.WriteString(WelcomeStyle.Render("muxd shell") + "\n\n")
	lines := []struct{ key, desc string }{
		{"exit", "Return to muxd chat"},
		{"/help", "Show this help"},
	}
	for _, l := range lines {
		b.WriteString("  " + FooterHead.Render(l.key) + "  " + FooterMeta.Render(l.desc) + "\n")
	}
	b.WriteString("\n" + FooterMeta.Render("  Windows commands (dir, type, etc.) are auto-detected."))
	b.WriteString("\n" + FooterMeta.Render("  PowerShell cmdlets (Get-Process, etc.) are auto-detected."))
	b.WriteString("\n" + FooterMeta.Render("  Git branch shown in header (green=clean, yellow=dirty)."))
	return b.String()
}

// handleShellCd changes the shell mode working directory.
func (m Model) handleShellCd(cmd string) (tea.Model, tea.Cmd) {
	// Strip "cd" prefix case-insensitively; handles "cd ..", "cd..", "CD\foo".
	target := strings.TrimSpace(cmd[2:])
	if target == "" || target == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return m, PrintToScrollback(ErrorLineStyle.Render("cd: " + err.Error()))
		}
		target = home
	}
	// Resolve relative paths against current shell cwd.
	if !filepath.IsAbs(target) {
		target = filepath.Join(m.shellCwd, target)
	}
	target = filepath.Clean(target)
	info, err := os.Stat(target)
	if err != nil {
		return m, PrintToScrollback(ErrorLineStyle.Render("cd: " + err.Error()))
	}
	if !info.IsDir() {
		return m, PrintToScrollback(ErrorLineStyle.Render("cd: not a directory: " + target))
	}
	m.shellCwd = target
	return m, nil
}

func (m Model) refreshCurrentSession() (tea.Model, tea.Cmd) {
	var (
		msgs []domain.TranscriptMessage
		err  error
	)
	if m.Daemon != nil {
		msgs, err = m.Daemon.GetMessages(m.Session.ID)
	} else if m.Store != nil {
		msgs, err = m.Store.GetMessages(m.Session.ID)
	} else {
		return m, PrintToScrollback(m.renderError("No session store available."))
	}
	if err != nil {
		return m, PrintToScrollback(m.renderError("Refresh failed: " + err.Error()))
	}

	oldLen := len(m.messages)
	if oldLen > len(msgs) {
		oldLen = 0
	}

	var newLines []string
	for _, msg := range msgs[oldLen:] {
		if msg.Role == "system" {
			continue
		}
		newLines = append(newLines, FormatBlockMessage(msg, m.width))
		if msg.Role == "user" && !msg.HasBlocks() {
			m.history = append(m.history, msg.Content)
		}
	}

	m.messages = msgs
	m.appendRuntimeLog(fmt.Sprintf("refresh: loaded=%d new=%d", len(msgs), len(msgs)-oldLen))
	if len(newLines) == 0 {
		return m, PrintToScrollback(WelcomeStyle.Render("No new messages."))
	}
	return m, PrintToScrollback(strings.Join(newLines, "\n\n"))
}

// SessionPickerMsg carries sessions for the picker overlay.
type SessionPickerMsg struct {
	Sessions []domain.Session
	Err      error
}

// BranchDoneMsg signals that a session branch completed.
type BranchDoneMsg struct {
	Session *domain.Session
	Err     error
}

// TelegramStartedMsg signals that the Telegram bot started successfully.
type TelegramStartedMsg struct{ BotName string }

// TelegramErrorMsg signals a Telegram bot error.
type TelegramErrorMsg struct{ Err error }

// XAuthDoneMsg signals completion of /x auth flow.
type XAuthDoneMsg struct {
	Token tools.XOAuthToken
	Err   error
}

func (m Model) handleBranchDone(msg BranchDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, PrintToScrollback(m.renderError("Branch failed: " + msg.Err.Error()))
	}
	m.Session = msg.Session
	m.messages = nil
	m.inputTokens = msg.Session.InputTokens
	m.outputTokens = msg.Session.OutputTokens
	m.lastInputTokens = 0
	m.lastOutputTokens = 0
	m.cacheCreationInputTokens = 0
	m.cacheReadInputTokens = 0
	m.lastCacheCreationInputTokens = 0
	m.lastCacheReadInputTokens = 0
	m.titled = true
	m.history = nil
	m.historyIdx = -1
	m.historyDraft = ""
	m.checkpoints = nil
	m.redoStack = nil
	m.resuming = true
	return m, tea.Batch(
		PrintToScrollback(WelcomeStyle.Render(fmt.Sprintf("Branched to new session %s", msg.Session.ID[:8]))),
		m.loadSessionHistory(),
	)
}

func (m Model) handleTelegram(args []string) (tea.Model, tea.Cmd) {
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "start":
		if m.telegramCancel != nil {
			return m, PrintToScrollback(WelcomeStyle.Render("Telegram bot is already running. Use /telegram stop first."))
		}
		return m, m.startTelegramBot()

	case "stop":
		if m.telegramCancel == nil {
			return m, PrintToScrollback(WelcomeStyle.Render("Telegram bot is not running."))
		}
		m.telegramCancel()
		m.telegramCancel = nil
		return m, PrintToScrollback(WelcomeStyle.Render("Telegram bot stopped."))

	case "status":
		if m.telegramCancel != nil {
			return m, PrintToScrollback(WelcomeStyle.Render("Telegram bot is running."))
		}
		return m, PrintToScrollback(WelcomeStyle.Render("Telegram bot is not running. Use /telegram start"))

	default:
		return m, PrintToScrollback(m.renderError("Usage: /telegram [start|stop|status]"))
	}
}

func (m Model) handleQRCommand(args []string) (tea.Model, tea.Cmd) {
	if m.Daemon == nil {
		return m, PrintToScrollback(m.renderError("No daemon connection available."))
	}

	// Fetch QR code from daemon
	qrURL := fmt.Sprintf("http://localhost:%d/api/qrcode?format=ascii", m.Daemon.Port())
	req, err := http.NewRequest("GET", qrURL, nil)
	if err != nil {
		return m, PrintToScrollback(m.renderError("Failed to create request: " + err.Error()))
	}
	req.Header.Set("Authorization", "Bearer "+m.Daemon.AuthToken())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return m, PrintToScrollback(m.renderError("Failed to fetch QR code: " + err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return m, PrintToScrollback(m.renderError(fmt.Sprintf("Server returned %d", resp.StatusCode)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return m, PrintToScrollback(m.renderError("Failed to read response: " + err.Error()))
	}

	// Build output with instructions
	var lines []string
	lines = append(lines, FooterHead.Render("Mobile Connection QR Code"))
	lines = append(lines, "")
	lines = append(lines, string(body))
	lines = append(lines, "")
	lines = append(lines, FooterMeta.Render("Scan this QR code with the muxd mobile app to connect."))
	lines = append(lines, FooterMeta.Render("The QR code contains: host, port, and authentication token."))

	// Show connection info
	lf, lfErr := daemon.ReadLockfile()
	if lfErr == nil {
		bindAddr := lf.BindAddr
		if bindAddr == "" {
			bindAddr = "localhost"
		}
		ips := daemon.GetLocalIPs()
		if len(ips) > 0 && (bindAddr == "0.0.0.0" || bindAddr == "") {
			lines = append(lines, "")
			lines = append(lines, FooterMeta.Render("Local IPs: "+strings.Join(ips, ", ")))
		}
		lines = append(lines, FooterMeta.Render(fmt.Sprintf("Server: %s:%d", bindAddr, lf.Port)))
	}

	return m, PrintToScrollback(strings.Join(lines, "\n"))
}

func (m *Model) startTelegramBot() tea.Cmd {
	cfg, err := config.LoadTelegramConfigFromPrefs(m.Prefs)
	if err != nil {
		return PrintToScrollback(m.renderError(err.Error()))
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.telegramCancel = cancel

	st := m.Store
	apiKey := m.APIKey
	modelID := m.modelID
	modelLabel := m.modelLabel
	prov := m.Provider
	prefs := m.Prefs

	return func() tea.Msg {
		adapter, err := telegram.NewAdapter(cfg, st, apiKey, modelID, modelLabel, prov, &prefs)
		if err != nil {
			cancel()
			return TelegramErrorMsg{Err: err}
		}

		botName := adapter.BotName()

		// Run in background — errors come back as messages
		go func() {
			if err := adapter.Run(ctx); err != nil && err != context.Canceled {
				if Prog != nil {
					Prog.Send(TelegramErrorMsg{Err: err})
				}
			}
		}()

		return TelegramStartedMsg{BotName: botName}
	}
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

// ---------------------------------------------------------------------------
// Message handlers
// ---------------------------------------------------------------------------

func (m Model) handlePaste(msg PasteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || m.thinking {
		return m, nil
	}
	pasted := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	pasted = strings.ReplaceAll(pasted, "\r", "\n")
	pasted = strings.TrimRight(pasted, "\n")
	if pasted != "" {
		m.insertInputAtCursor(pasted)
		m.resetHistory()
	}
	return m, nil
}

func (m Model) handleClipboardWrite(msg ClipboardWriteMsg) (tea.Model, tea.Cmd) {
	var sysText string
	if msg.Err != nil {
		sysText = "Copy failed: " + msg.Err.Error()
	} else if msg.OK {
		sysText = "Copied to clipboard."
	}
	if sysText != "" {
		sys := domain.TranscriptMessage{Role: "system", Content: sysText}
		m.messages = append(m.messages, sys)
		return m, PrintToScrollback(FormatMessageForScrollback(sys, m.width))
	}
	return m, nil
}

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
// Daemon streaming
// ---------------------------------------------------------------------------

// StreamViaDaemon sends a message to the daemon via HTTP SSE and dispatches
// events to the TUI via Prog.Send().
func StreamViaDaemon(d *daemon.DaemonClient, sessionID, text string) tea.Cmd {
	return func() tea.Msg {
		if d == nil {
			return StreamDoneMsg{Err: fmt.Errorf("no daemon connection")}
		}
		err := d.Submit(sessionID, text, func(evt daemon.SSEEvent) {
			if Prog == nil {
				return
			}
			switch evt.Type {
			case "delta":
				Prog.Send(StreamDeltaMsg{Text: evt.DeltaText})
			case "tool_start":
				Prog.Send(ToolStatusMsg{Name: evt.ToolName, Status: "running"})
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
				Prog.Send(CompactedMsg{})
			case "titled":
				Prog.Send(TitledMsg{Title: evt.Title, Tags: evt.Tags})
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
// Context compaction
// ---------------------------------------------------------------------------

const (
	// CompactThreshold is the token count threshold for compaction.
	CompactThreshold = 100_000
	// CompactKeepTail is the number of recent messages to keep.
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
	if _, err := fmt.Fprintf(f, "%s %s\n", ts, msg); err != nil {
		// log file write failure; nothing further to do
	}
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
