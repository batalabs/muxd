package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/tools"
)

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

	case "/nodes":
		if m.hubBaseURL == "" {
			return m, PrintToScrollback(m.renderError("Not connected to a hub. Use --remote to connect."))
		}
		return m, m.openNodePicker()

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
		if m.Daemon != nil {
			// Use the daemon endpoint which also marks agent as user-renamed.
			if err := m.Daemon.RenameSession(m.Session.ID, newTitle); err != nil {
				fmt.Fprintf(os.Stderr, "tui: rename via daemon: %v\n", err)
			}
		} else if m.Store != nil {
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

	case "/qr":
		return m.handleQRCommand(parts[1:])

	case "/emoji":
		m.emojiPicker = NewEmojiPicker(m.Prefs.FooterEmoji)
		return m, nil

	case "/remember":
		return m.handleRememberCommand(parts[1:])

	case "/tools":
		return m.handleToolsCommand(parts[1:])

	case "/schedule":
		return m.handleScheduleCommand(parts[1:])

	case "/consult":
		question := strings.TrimSpace(strings.TrimPrefix(clean, "/consult"))
		if question == "" {
			// Auto-build from last user+assistant messages in m.messages
			var lastUser, lastAsst string
			for _, msg := range m.messages {
				if msg.Role == "user" && msg.Content != "" {
					lastUser = msg.Content
				} else if msg.Role == "assistant" && msg.Content != "" {
					lastAsst = msg.Content
				}
			}
			if lastUser == "" && lastAsst == "" {
				return m, PrintToScrollback(m.renderError("No conversation to consult about"))
			}
			if lastUser != "" {
				question = "User asked: " + lastUser
			}
			if lastAsst != "" {
				if question != "" {
					question += "\n\nAssistant responded: " + lastAsst
				} else {
					question = "Assistant responded: " + lastAsst
				}
			}
		}
		if m.Daemon == nil {
			return m, PrintToScrollback(m.renderError("Consult requires a daemon connection."))
		}
		return m, ConsultCmd(m.Daemon, m.Session.ID, question)

	case "/help":
		cmds := domain.CommandHelp()
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
		return m, PrintToScrollback(WelcomeStyle.Render("Canceled scheduled job: " + args[1]))
	case "add":
		if len(args) < 4 {
			return m, PrintToScrollback(m.renderError("Usage: /schedule add <tool> <HH:MM|RFC3339> <json> [--daily|--hourly]"))
		}
		toolName := tools.NormalizeToolName(args[1])
		if _, ok := tools.FindTool(toolName); !ok {
			return m, PrintToScrollback(m.renderError("Unknown tool: " + toolName))
		}
		scheduledFor, err := tools.ParseScheduleTime(args[2], time.Now())
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
		scheduledFor, err := tools.ParseScheduleTime(args[1], time.Now())
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
