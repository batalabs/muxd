package tui

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
	"github.com/batalabs/muxd/internal/diff"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/hub"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/tools"
)

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
	m.turnToolCount++
	m.turnCurrentTool = describeToolStart(msg.Name, msg.Input)
	m.toolStatus = "Running " + msg.Name + "..."
	m.appendRuntimeLog("tool_start: " + msg.Name)
	return m, nil
}

func (m Model) handleToolResult(msg ToolResultMsg) (tea.Model, tea.Cmd) {
	m.toolStatus = fmt.Sprintf("Finished %s, waiting for model...", msg.Name)
	m.turnCurrentTool = ""
	// Set human-readable last action for status display.
	m.turnLastAction = describeToolAction(msg.Name, msg.Result)
	// Track file changes for status display.
	if msg.Name == "file_edit" || msg.Name == "file_write" {
		if m.turnFilesChanged == nil {
			m.turnFilesChanged = make(map[string]bool)
		}
		// Extract filename from result (first word or path-like string).
		if parts := strings.Fields(msg.Result); len(parts) > 0 {
			for _, p := range parts {
				if strings.Contains(p, ".") && (strings.Contains(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, ".go") || strings.Contains(p, ".ts") || strings.Contains(p, ".py")) {
					m.turnFilesChanged[p] = true
					break
				}
			}
		}
	}
	m.appendRuntimeLog(fmt.Sprintf("tool_done: %s error=%t bytes=%d", msg.Name, msg.IsError, len(msg.Result)))
	result := msg.Result
	if m.Prefs.HideDiffs {
		if idx := strings.Index(result, diff.DiffSentinel); idx != -1 {
			result = result[:idx]
		}
	}
	resultFormatted := FormatToolResult(msg.Name, result, msg.IsError, max(20, m.width-4))
	return m, PrintToScrollback(resultFormatted)
}

func (m Model) handleTurnDone(msg TurnDoneMsg) (tea.Model, tea.Cmd) {
	m.thinking = false
	m.toolStatus = ""
	m.turnToolCount = 0
	m.turnFilesChanged = nil
	m.turnCurrentTool = ""
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

// handleNodePickerKey intercepts all keys when the node picker is active.
func (m Model) handleNodePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape, tea.KeyCtrlC:
		m.nodePicker.Dismiss()
		// If no session exists (initial hub connect), quit
		if m.Session == nil {
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyEnter:
		node := m.nodePicker.SelectedNode()
		if node == nil {
			return m, nil
		}
		m.nodePicker.Dismiss()

		// Set DaemonClient baseURL to proxy through hub
		proxyURL := fmt.Sprintf("%s/api/hub/proxy/%s", m.hubBaseURL, node.ID)
		m.Daemon.SetBaseURL(proxyURL)

		// If no session yet, create one on the selected node
		if m.Session == nil {
			cwd, _ := os.Getwd()
			sessionID, err := m.Daemon.CreateSession(cwd, m.modelID)
			if err != nil {
				return m, PrintToScrollback(m.renderError("Failed to create session on node: " + err.Error()))
			}
			sess, err := m.Daemon.GetSession(sessionID)
			if err != nil {
				return m, PrintToScrollback(m.renderError("Failed to load session: " + err.Error()))
			}
			m.Session = sess
			m.viewLines = []string{WelcomeStyle.Render(fmt.Sprintf("Connected to node %s. One prompt away from wizardry.", node.Name))}
		} else {
			m.viewLines = append(m.viewLines, WelcomeStyle.Render(fmt.Sprintf("Switched to node %s.", node.Name)))
		}
		return m, nil

	case tea.KeyUp:
		m.nodePicker.MoveUp()
		return m, nil

	case tea.KeyDown:
		m.nodePicker.MoveDown()
		return m, nil

	case tea.KeyBackspace, tea.KeyDelete:
		m.nodePicker.BackspaceFilter()
		return m, nil

	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				m.nodePicker.AppendFilter(r)
			}
		}
		return m, nil
	}
}

// openNodePicker fetches nodes from the hub and opens the picker.
func (m Model) openNodePicker() tea.Cmd {
	baseURL := m.hubBaseURL
	token := m.hubToken
	return func() tea.Msg {
		hc := hub.NewHubClient(baseURL, token)
		nodes, err := hc.ListNodes()
		return NodePickerMsg{Nodes: nodes, Err: err}
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
			if m.Daemon != nil {
				if err := m.Daemon.RenameSession(id, newTitle); err != nil {
					fmt.Fprintf(os.Stderr, "tui: rename via daemon: %v\n", err)
				}
			} else if m.Store != nil {
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
				return m, PrintToScrollback(WelcomeStyle.Render("Canceled tool changes."))
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

func (m Model) handleEmojiPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.emojiPicker.Dismiss()
		m.emojiPicker = nil
		return m, nil
	case tea.KeyUp:
		m.emojiPicker.MoveUp()
		return m, nil
	case tea.KeyDown:
		m.emojiPicker.MoveDown()
		return m, nil
	case tea.KeyEnter:
		emoji := m.emojiPicker.Selected()
		name := m.emojiPicker.SelectedName()
		m.Prefs.FooterEmoji = emoji
		m.emojiPicker.Dismiss()
		m.emojiPicker = nil
		_ = config.SavePreferences(m.Prefs)
		if name == "none" {
			return m, PrintToScrollback(WelcomeStyle.Render("Footer emoji removed."))
		}
		return m, PrintToScrollback(WelcomeStyle.Render("Footer emoji set to " + emoji + " (" + name + ")."))
	default:
		return m, nil
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
			if key == "footer.emoji" {
				m.configPicker.Dismiss()
				m.configPicker = nil
				m.emojiPicker = NewEmojiPicker(m.Prefs.FooterEmoji)
				return m, nil
			}
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
	// API key changed -update daemon and local state
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
	// Model changed -resolve provider, update runtime
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
		// Keep prefs.Provider in sync so restarts resolve correctly.
		m.Prefs.Provider = newProvName
		if err := config.SavePreferences(m.Prefs); err != nil {
			fmt.Fprintf(os.Stderr, "tui: save prefs: %v\n", err)
		}
		if m.Daemon != nil && m.Session != nil {
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
	if key == "zai.coding_plan" {
		b, _ := config.ParseBoolish(value)
		provider.SetZAICodingPlan(b)
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
		// Warn if the resolved provider has no API key -the user likely
		// intended a different provider but typed a bare model name.
		if resolvedProv != "ollama" && !strings.Contains(value, "/") {
			if _, keyErr := config.LoadProviderAPIKey(m.Prefs, resolvedProv); keyErr != nil {
				return fmt.Errorf("model %q resolved to %s but no API key is set; use %s/%s to specify provider explicitly",
					value, resolvedProv, resolvedProv, value)
			}
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
	case "anthropic.api_key", "zai.api_key", "grok.api_key", "mistral.api_key", "openai.api_key", "google.api_key", "brave.api_key", "fireworks.api_key", "textbelt.api_key":
		return ""
	default:
		return m.Prefs.Get(key)
	}
}

func (m Model) handleQRCommand(args []string) (tea.Model, tea.Cmd) {
	if m.Daemon == nil {
		return m, PrintToScrollback(m.renderError("No daemon connection available."))
	}

	// /qr new -regenerate token before showing QR code
	if len(args) > 0 && args[0] == "new" {
		regenURL := fmt.Sprintf("http://localhost:%d/api/qrcode/regenerate", m.Daemon.Port())
		req, err := http.NewRequest("POST", regenURL, nil)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Failed to create request: " + err.Error()))
		}
		req.Header.Set("Authorization", "Bearer "+m.Daemon.AuthToken())
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return m, PrintToScrollback(m.renderError("Failed to regenerate token: " + err.Error()))
		}
		resp.Body.Close()
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

		// Show token for manual entry
		lines = append(lines, "")
		lines = append(lines, FooterMeta.Render("Token: "+m.Daemon.AuthToken()))
	}

	return m, PrintToScrollback(strings.Join(lines, "\n"))
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
