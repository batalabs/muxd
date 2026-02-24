package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/time/rate"

	"github.com/batalabs/muxd/internal/agent"
	"github.com/batalabs/muxd/internal/checkpoint"
	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"
	"github.com/batalabs/muxd/internal/tools"
)

// AnsiRegex matches ANSI escape sequences for stripping from Telegram output.
var AnsiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape codes from a string.
func StripANSI(s string) string {
	return AnsiRegex.ReplaceAllString(s, "")
}

// ---------------------------------------------------------------------------
// Telegram adapter -- drives agent.Service via the Telegram Bot API
// ---------------------------------------------------------------------------

const (
	// MaxMessageLen is the maximum Telegram message length.
	MaxMessageLen = 4096
	// EditInterval is the minimum time between message edits.
	EditInterval = 2 * time.Second
)

// Adapter manages a Telegram bot that maps each chat to an agent.Service.
type Adapter struct {
	bot    *tgbotapi.BotAPI
	Config config.TelegramConfig

	Store      *store.Store
	APIKey     string
	ModelID    string
	ModelLabel string
	Provider   provider.Provider
	Prefs      *config.Preferences

	mu           sync.Mutex
	sessions     map[int64]*agent.Service // chatID -> agent
	askState     map[int64]chan<- string  // chatID -> pending ask_user response
	rateLimiters map[int64]*rate.Limiter  // per-user rate limiters
	logMu        sync.Mutex
	logPath      string

	mcpManager *mcp.Manager // MCP server connections for tool routing

	cancel context.CancelFunc // set by Run; allows /stop to trigger shutdown
}

type telegramBotLogger struct {
	adapter *Adapter
}

func (l *telegramBotLogger) Println(v ...interface{}) {
	if l == nil || l.adapter == nil {
		return
	}
	l.adapter.logf("telegram_api: %s", strings.TrimSpace(fmt.Sprint(v...)))
}

func (l *telegramBotLogger) Printf(format string, v ...interface{}) {
	if l == nil || l.adapter == nil {
		return
	}
	l.adapter.logf("telegram_api: "+format, v...)
}

// NewAdapter creates and starts a Telegram bot adapter.
func NewAdapter(cfg config.TelegramConfig, st *store.Store, apiKey, modelID, modelLabel string, prov provider.Provider, prefs *config.Preferences) (*Adapter, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("connecting to Telegram: %w", err)
	}
	a := &Adapter{
		bot:          bot,
		Config:       cfg,
		Store:        st,
		APIKey:       apiKey,
		ModelID:      modelID,
		ModelLabel:   modelLabel,
		Provider:     prov,
		Prefs:        prefs,
		sessions:     make(map[int64]*agent.Service),
		askState:     make(map[int64]chan<- string),
		rateLimiters: make(map[int64]*rate.Limiter),
		logPath:      defaultTelegramLogPath(),
	}
	// Redirect library polling logs (e.g. transient 502/Bad Gateway) to runtime
	// file logs so they don't interfere with the TUI.
	if err := tgbotapi.SetLogger(&telegramBotLogger{adapter: a}); err != nil {
		fmt.Fprintf(os.Stderr, "telegram: set logger: %v\n", err)
	}

	// Initialize MCP servers so Telegram agents get the same tools as the TUI.
	a.initMCP()

	return a, nil
}

// BotName returns the bot's username (without the @ prefix).
func (ta *Adapter) BotName() string {
	return ta.bot.Self.UserName
}

// Run starts the long-polling loop. Blocks until the context is cancelled,
// /stop is issued, or the updates channel is closed.
func (ta *Adapter) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	ta.cancel = cancel

	// Note: do NOT print to stderr here. If the TUI is running, raw stderr
	// writes corrupt Bubble Tea's line tracking and break re-rendering.

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := ta.bot.GetUpdatesChan(u)

	go func() {
		<-ctx.Done()
		ta.bot.StopReceivingUpdates()
	}()

	for update := range updates {
		if update.Message == nil {
			continue
		}
		go ta.handleMessage(update.Message)
	}

	// Bot stopped.
	return ctx.Err()
}

// IsPrivateChat returns true if the chat is a private (one-on-one) chat.
func IsPrivateChat(chat *tgbotapi.Chat) bool {
	return chat != nil && chat.Type == "private"
}

// IsAllowed checks whether the given user ID is in the allowed list.
// An empty allowed list denies everyone.
func IsAllowed(userID int64, allowedIDs []int64) bool {
	for _, id := range allowedIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// allowRequest checks the per-user rate limiter. Returns true if the
// request is allowed, false if the user is being rate-limited.
func (ta *Adapter) allowRequest(userID int64) bool {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	rl, ok := ta.rateLimiters[userID]
	if !ok {
		// 1 request per second, burst of 5
		rl = rate.NewLimiter(rate.Limit(1.0), 5)
		ta.rateLimiters[userID] = rl
	}
	return rl.Allow()
}

func (ta *Adapter) handleMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	// Reject non-private chats (groups, supergroups, channels)
	if !IsPrivateChat(msg.Chat) {
		ta.logf("telegram: rejected %s chat %d", msg.Chat.Type, chatID)
		reply := tgbotapi.NewMessage(chatID, "This bot only works in private messages.")
		ta.bot.Send(reply)
		return
	}

	userID := msg.From.ID

	if !IsAllowed(userID, ta.Config.AllowedIDs) {
		username := ""
		if msg.From != nil {
			username = msg.From.UserName
		}
		ta.logf("telegram: unauthorized user %d (%s) attempted access", userID, username)
		reply := tgbotapi.NewMessage(chatID, "You are not authorized to use this bot.")
		ta.bot.Send(reply)
		return
	}

	// Rate limit check
	if !ta.allowRequest(userID) {
		reply := tgbotapi.NewMessage(chatID, "Too many requests. Please wait a moment.")
		ta.bot.Send(reply)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Intercept response if agent is waiting for ask_user answer
	ta.mu.Lock()
	if ch, ok := ta.askState[chatID]; ok {
		delete(ta.askState, chatID)
		ta.mu.Unlock()
		ch <- text
		return
	}
	ta.mu.Unlock()

	// Handle commands
	if strings.HasPrefix(text, "/") {
		ta.handleCommand(chatID, text)
		return
	}

	// Regular message -> agent
	svc := ta.GetOrCreateAgent(chatID)

	if svc.IsRunning() {
		reply := tgbotapi.NewMessage(chatID, "Still processing your previous message. Please wait.")
		ta.bot.Send(reply)
		return
	}

	ta.runAgent(chatID, svc, text)
}

func (ta *Adapter) handleCommand(chatID int64, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/start":
		welcome := "Welcome to muxd! Send me a message and I'll help you with coding tasks."
		reply := tgbotapi.NewMessage(chatID, welcome)
		ta.bot.Send(reply)

	case "/new":
		svc := ta.GetOrCreateAgent(chatID)
		if err := svc.NewSession(ta.projectPath()); err != nil {
			ta.logf("telegram: new session error: %v", err)
			reply := tgbotapi.NewMessage(chatID, "Failed to create session. "+sanitizeError(err))
			ta.bot.Send(reply)
			return
		}
		reply := tgbotapi.NewMessage(chatID, "New session started.")
		ta.bot.Send(reply)

	case "/sessions":
		sessions, err := ta.Store.ListSessions(ta.projectPath(), 20)
		if err != nil {
			ta.logf("telegram: list sessions error: %v", err)
			reply := tgbotapi.NewMessage(chatID, "Failed to list sessions. "+sanitizeError(err))
			ta.bot.Send(reply)
			return
		}
		if len(sessions) == 0 {
			reply := tgbotapi.NewMessage(chatID, "No sessions found.")
			ta.bot.Send(reply)
			return
		}
		var lines []string
		lines = append(lines, "Recent sessions:")
		for _, s := range sessions {
			title := strings.TrimSpace(s.Title)
			if title == "" {
				title = "New Session"
			}
			if len(title) > 48 {
				title = title[:48] + "..."
			}
			lines = append(lines, fmt.Sprintf("  %s  %s", s.ID[:8], title))
		}
		lines = append(lines, "", "Use /continue <id-prefix> to switch.")
		reply := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
		ta.bot.Send(reply)

	case "/continue", "/resume":
		if len(parts) < 2 {
			reply := tgbotapi.NewMessage(chatID, "Usage: /continue <session-id-prefix>")
			ta.bot.Send(reply)
			return
		}
		prefix := strings.TrimSpace(parts[1])
		sess, err := ta.Store.FindSessionByPrefix(prefix)
		if err != nil {
			reply := tgbotapi.NewMessage(chatID, "Session not found.")
			ta.bot.Send(reply)
			return
		}
		svc := ta.newServiceForSession(sess)
		ta.mu.Lock()
		ta.sessions[chatID] = svc
		ta.mu.Unlock()
		reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Switched to session %s (%s).", sess.ID[:8], sess.Title))
		ta.bot.Send(reply)

	case "/rename":
		if len(parts) < 2 {
			reply := tgbotapi.NewMessage(chatID, "Usage: /rename <new title>")
			ta.bot.Send(reply)
			return
		}
		svc := ta.GetOrCreateAgent(chatID)
		if svc.IsRunning() {
			reply := tgbotapi.NewMessage(chatID, "Still processing your previous message. Please wait.")
			ta.bot.Send(reply)
			return
		}
		sess := svc.Session()
		if sess == nil {
			reply := tgbotapi.NewMessage(chatID, "No active session.")
			ta.bot.Send(reply)
			return
		}
		newTitle := strings.TrimSpace(strings.Join(parts[1:], " "))
		if newTitle == "" {
			reply := tgbotapi.NewMessage(chatID, "Usage: /rename <new title>")
			ta.bot.Send(reply)
			return
		}
		if ta.Store != nil {
			if err := ta.Store.UpdateSessionTitle(sess.ID, newTitle); err != nil {
				ta.logf("telegram: rename session error: %v", err)
				reply := tgbotapi.NewMessage(chatID, "Rename failed. "+sanitizeError(err))
				ta.bot.Send(reply)
				return
			}
		}
		sess.Title = newTitle
		reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Session renamed to: %s", newTitle))
		ta.bot.Send(reply)

	case "/branch":
		svc := ta.GetOrCreateAgent(chatID)
		if svc.IsRunning() {
			reply := tgbotapi.NewMessage(chatID, "Still processing your previous message. Please wait.")
			ta.bot.Send(reply)
			return
		}
		curr := svc.Session()
		if curr == nil {
			reply := tgbotapi.NewMessage(chatID, "No active session.")
			ta.bot.Send(reply)
			return
		}
		if ta.Store == nil {
			reply := tgbotapi.NewMessage(chatID, "Branch unavailable: no store configured.")
			ta.bot.Send(reply)
			return
		}
		branched, err := ta.Store.BranchSession(curr.ID, 0)
		if err != nil {
			ta.logf("telegram: branch session error: %v", err)
			reply := tgbotapi.NewMessage(chatID, "Branch failed. "+sanitizeError(err))
			ta.bot.Send(reply)
			return
		}
		newSvc := ta.newServiceForSession(branched)
		ta.mu.Lock()
		ta.sessions[chatID] = newSvc
		ta.mu.Unlock()
		reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Branched to new session %s (%s).", branched.ID[:8], branched.Title))
		ta.bot.Send(reply)

	case "/config":
		args := parts[1:]
		result, err := config.ExecuteConfigAction(ta.Prefs, args)
		if err != nil {
			ta.logf("telegram: config error: %v", err)
			reply := tgbotapi.NewMessage(chatID, "Config error. "+sanitizeError(err))
			ta.bot.Send(reply)
			return
		}
		if len(args) >= 3 && strings.ToLower(args[0]) == "set" && strings.ToLower(args[1]) == "model" {
			spec := strings.Join(args[2:], " ")
			ta.applyModelSpec(spec)
		}
		if len(args) >= 3 && strings.ToLower(args[0]) == "set" && strings.HasPrefix(strings.ToLower(args[1]), "x.") {
			ta.applyXOAuthFromPrefs()
		}
		if len(args) >= 3 && strings.ToLower(args[0]) == "set" && strings.ToLower(args[1]) == "tools.disabled" {
			ta.applyDisabledToolsFromPrefs()
		}
		reply := tgbotapi.NewMessage(chatID, result)
		ta.bot.Send(reply)

	case "/tools":
		sub := "list"
		if len(parts) > 1 {
			sub = strings.ToLower(strings.TrimSpace(parts[1]))
		}
		disabled := ta.Prefs.DisabledToolsSet()
		names := tools.ToolNames()
		switch sub {
		case "list":
			var lines []string
			lines = append(lines, "Tools:")
			for _, name := range names {
				displayName := tools.ToolDisplayName(name)
				state := "enabled"
				if disabled[name] {
					state = "disabled"
				}
				lines = append(lines, fmt.Sprintf("  %-20s %s", displayName, state))
			}
			lines = append(lines, "", "Usage: /tools [list|enable <name>|disable <name>|toggle <name>|profile <safe|coder|research>]")
			ta.bot.Send(tgbotapi.NewMessage(chatID, strings.Join(lines, "\n")))
		case "profile":
			if len(parts) < 3 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /tools profile <safe|coder|research>"))
				return
			}
			profile := strings.ToLower(strings.TrimSpace(parts[2]))
			if profile != "safe" && profile != "coder" && profile != "research" {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Unknown profile: "+profile))
				return
			}
			disabled = tools.ToolProfileDisabledSet(profile)
			ta.Prefs.ToolsDisabled = disabledToolsCSV(disabled)
			if err := config.SavePreferences(*ta.Prefs); err != nil {
				ta.logf("telegram: saving tools profile failed: %v", err)
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to save tools config. "+sanitizeError(err)))
				return
			}
			ta.applyDisabledToolsFromPrefs()
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Applied tools profile: "+profile))
		case "enable", "disable", "toggle":
			if len(parts) < 3 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /tools "+sub+" <tool_name>"))
				return
			}
			name := tools.NormalizeToolName(parts[2])
			if _, ok := tools.FindTool(name); !ok {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Unknown tool: "+name))
				return
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
			ta.Prefs.ToolsDisabled = disabledToolsCSV(disabled)
			if err := config.SavePreferences(*ta.Prefs); err != nil {
				ta.logf("telegram: saving tools.disabled failed: %v", err)
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to save tools config. "+sanitizeError(err)))
				return
			}
			ta.applyDisabledToolsFromPrefs()
			state := "enabled"
			if disabled[name] {
				state = "disabled"
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Tool %s is now %s.", name, state)))
		default:
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /tools [list|enable <name>|disable <name>|toggle <name>|profile <safe|coder|research>]"))
		}

	case "/tweet":
		args := parts[1:]
		if ta.Store == nil {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Tweet scheduler unavailable: no store configured."))
			return
		}
		if len(args) == 0 {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /tweet <text> | /tweet --schedule <HH:MM|RFC3339> [--daily|--hourly] <text> | /tweet --list | /tweet --cancel <id>"))
			return
		}
		if args[0] == "--list" {
			items, err := ta.Store.ListScheduledToolJobs(100)
			if err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to list scheduled tweets."))
				return
			}
			var xJobs []store.ScheduledToolJob
			for _, it := range items {
				if it.ToolName == "x_post" {
					xJobs = append(xJobs, it)
				}
			}
			if len(xJobs) == 0 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "No scheduled tweets."))
				return
			}
			var lines []string
			lines = append(lines, "Scheduled tweets:")
			for _, it := range xJobs {
				id := it.ID
				if len(id) > 8 {
					id = id[:8]
				}
				text := ""
				if v, ok := it.ToolInput["text"].(string); ok {
					text = v
				}
				lines = append(lines, fmt.Sprintf("  %-8s %-9s %-9s %s", id, it.ScheduledFor.Local().Format("2006-01-02 15:04"), it.Status, summarizeTelegramText(text)))
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, strings.Join(lines, "\n")))
			return
		}
		if args[0] == "--cancel" {
			if len(args) < 2 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /tweet --cancel <id>"))
				return
			}
			if err := ta.Store.CancelScheduledToolJob(args[1]); err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to cancel scheduled tweet."))
				return
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Cancelled scheduled tweet: "+args[1]))
			return
		}

		recurrence := "once"
		scheduleRaw := ""
		i := 0
		for i < len(args) && strings.HasPrefix(args[i], "--") {
			switch args[i] {
			case "--schedule":
				if i+1 >= len(args) {
					ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /tweet --schedule <HH:MM|RFC3339> [--daily|--hourly] <text>"))
					return
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
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Unknown flag: "+args[i]))
				return
			}
		}
		text := strings.TrimSpace(strings.Join(args[i:], " "))
		if text == "" {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Tweet text cannot be empty."))
			return
		}
		if scheduleRaw == "" {
			token := strings.TrimSpace(os.Getenv("X_BEARER_TOKEN"))
			id, url, err := tools.PostXTweet(text, token)
			if err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Tweet failed: "+err.Error()))
				return
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Posted tweet %s\n%s", id, url)))
			return
		}
		scheduledFor, err := tools.ParseTweetScheduleTime(scheduleRaw, time.Now())
		if err != nil {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Invalid schedule time: "+err.Error()))
			return
		}
		id, err := ta.Store.CreateScheduledToolJob("x_post", map[string]any{"text": text}, scheduledFor, recurrence)
		if err != nil {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to schedule tweet."))
			return
		}
		shortID := id
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		ta.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Scheduled tweet %s for %s (%s)", shortID, scheduledFor.Local().Format("2006-01-02 15:04"), recurrence)))
		return

	case "/schedule":
		args := parts[1:]
		if ta.Store == nil {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Scheduler unavailable: no store configured."))
			return
		}
		if len(args) == 0 {
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /schedule add <tool> <HH:MM|RFC3339> <json> [--daily|--hourly] | /schedule list | /schedule cancel <id>"))
			return
		}
		switch strings.ToLower(args[0]) {
		case "list":
			items, err := ta.Store.ListScheduledToolJobs(100)
			if err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to list scheduled jobs."))
				return
			}
			if len(items) == 0 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "No scheduled jobs."))
				return
			}
			var lines []string
			lines = append(lines, "Scheduled jobs:")
			for _, it := range items {
				id := it.ID
				if len(id) > 8 {
					id = id[:8]
				}
				lines = append(lines, fmt.Sprintf("  %-8s %-14s %-9s %s", id, it.ToolName, it.Status, it.ScheduledFor.Local().Format("2006-01-02 15:04")))
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, strings.Join(lines, "\n")))
		case "cancel":
			if len(args) < 2 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /schedule cancel <id>"))
				return
			}
			if err := ta.Store.CancelScheduledToolJob(args[1]); err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to cancel scheduled job."))
				return
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Cancelled scheduled job: "+args[1]))
		case "add":
			if len(args) < 4 {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /schedule add <tool> <HH:MM|RFC3339> <json> [--daily|--hourly]"))
				return
			}
			toolName := tools.NormalizeToolName(args[1])
			if _, ok := tools.FindTool(toolName); !ok {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Unknown tool: "+toolName))
				return
			}
			scheduledFor, err := tools.ParseTweetScheduleTime(args[2], time.Now())
			if err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Invalid schedule time: "+err.Error()))
				return
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
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Invalid JSON input: "+err.Error()))
				return
			}
			id, err := ta.Store.CreateScheduledToolJob(toolName, input, scheduledFor, recurrence)
			if err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to schedule job."))
				return
			}
			shortID := id
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Scheduled job %s for %s (%s)", shortID, scheduledFor.Local().Format("2006-01-02 15:04"), recurrence)))
		default:
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /schedule [add|list|cancel]"))
		}

	case "/x":
		sub := "status"
		if len(parts) > 1 {
			sub = strings.ToLower(strings.TrimSpace(parts[1]))
		}
		switch sub {
		case "status":
			if strings.TrimSpace(ta.Prefs.XAccessToken) == "" {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "X auth: not connected. Run /x auth in TUI."))
				return
			}
			msg := "X auth: connected"
			if exp := strings.TrimSpace(ta.Prefs.XTokenExpiry); exp != "" {
				msg += " (expires " + exp + ")"
			}
			ta.bot.Send(tgbotapi.NewMessage(chatID, msg))
		case "logout":
			ta.Prefs.XAccessToken = ""
			ta.Prefs.XRefreshToken = ""
			ta.Prefs.XTokenExpiry = ""
			if err := config.SavePreferences(*ta.Prefs); err != nil {
				ta.bot.Send(tgbotapi.NewMessage(chatID, "Failed to clear X auth."))
				return
			}
			ta.applyXOAuthFromPrefs()
			ta.bot.Send(tgbotapi.NewMessage(chatID, "X auth cleared."))
		case "auth":
			ta.bot.Send(tgbotapi.NewMessage(chatID, "For now, run /x auth in muxd TUI on your local machine to complete browser callback authentication."))
		default:
			ta.bot.Send(tgbotapi.NewMessage(chatID, "Usage: /x [auth|status|logout]"))
		}

	case "/help":
		cmds := domain.CommandHelp(true)
		var lines []string
		lines = append(lines, "Commands:")
		for _, c := range cmds {
			lines = append(lines, fmt.Sprintf("  %-18s %s", c.Name, c.Description))
		}
		reply := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
		ta.bot.Send(reply)

	case "/stop":
		reply := tgbotapi.NewMessage(chatID, "Bot shutting down...")
		ta.bot.Send(reply)
		// Cancel any running agent sessions
		ta.mu.Lock()
		for _, svc := range ta.sessions {
			svc.Cancel()
		}
		ta.mu.Unlock()
		if ta.cancel != nil {
			ta.cancel()
		}

	default:
		reply := tgbotapi.NewMessage(chatID, "Unknown command. Try /help for available commands.")
		ta.bot.Send(reply)
	}
}

// sanitizeError returns a generic error message for user-facing output.
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return "An internal error occurred."
}

func summarizeTelegramText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= 72 {
		return s
	}
	return s[:72] + "..."
}

func defaultTelegramLogPath() string {
	dir, err := config.DataDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "runtime-telegram.log")
}

func (ta *Adapter) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if line == "" {
		return
	}
	path := ta.logPath
	if path == "" {
		path = defaultTelegramLogPath()
	}
	if path == "" {
		return
	}
	ta.logMu.Lock()
	defer ta.logMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC3339)
	if _, err := fmt.Fprintf(f, "[%s] %s\n", ts, line); err != nil {
		// log file write failure; nothing further to do
	}
}

// projectPath returns the current working directory as the project path
// for new Telegram sessions.
func (ta *Adapter) projectPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// GetOrCreateAgent returns the agent.Service for the given chat ID, creating
// a new one if needed.
func (ta *Adapter) GetOrCreateAgent(chatID int64) *agent.Service {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	if svc, ok := ta.sessions[chatID]; ok {
		return svc
	}

	projectPath := ta.projectPath()

	sess, err := ta.Store.CreateSession(projectPath, ta.ModelID)
	if err != nil {
		// Fallback: create agent without store persistence
		sess = &domain.Session{ID: domain.NewUUID(), ProjectPath: projectPath}
	}

	svc := ta.newServiceForSession(sess)

	ta.sessions[chatID] = svc
	return svc
}

func (ta *Adapter) newServiceForSession(sess *domain.Session) *agent.Service {
	svc := agent.NewService(ta.APIKey, ta.ModelID, ta.ModelLabel, ta.Store, sess, ta.Provider)

	// Pass Brave API key from preferences
	if ta.Prefs != nil && ta.Prefs.BraveAPIKey != "" {
		svc.SetBraveAPIKey(ta.Prefs.BraveAPIKey)
	}
	if ta.Prefs != nil {
		svc.SetDisabledTools(ta.Prefs.DisabledToolsSet())
		svc.SetXOAuth(
			ta.Prefs.XClientID,
			ta.Prefs.XClientSecret,
			ta.Prefs.XAccessToken,
			ta.Prefs.XRefreshToken,
			ta.Prefs.XTokenExpiry,
			func(accessToken, refreshToken, tokenExpiry string) error {
				ta.mu.Lock()
				defer ta.mu.Unlock()
				if ta.Prefs == nil {
					return nil
				}
				ta.Prefs.XAccessToken = accessToken
				ta.Prefs.XRefreshToken = refreshToken
				ta.Prefs.XTokenExpiry = tokenExpiry
				return config.SavePreferences(*ta.Prefs)
			},
		)
	}

	// Propagate MCP manager so Telegram agents can use MCP tools.
	// NOTE: caller (GetOrCreateAgent) already holds ta.mu — read directly.
	if ta.mcpManager != nil {
		svc.SetMCPManager(ta.mcpManager)
	}

	// Detect git repo for checkpoints
	if root, ok := checkpoint.DetectGitRepo(); ok {
		svc.SetGitAvailable(true, root)
	}
	return svc
}

// initMCP loads .mcp.json config and starts MCP server connections.
func (ta *Adapter) initMCP() {
	cwd, _ := tools.Getwd()
	cfg, err := mcp.LoadMCPConfig(cwd)
	if err != nil {
		ta.logf("telegram: mcp config error: %v", err)
		return
	}
	if len(cfg.MCPServers) == 0 {
		return
	}

	mgr := mcp.NewManager()
	ctx := context.Background()
	if err := mgr.StartAll(ctx, cfg); err != nil {
		ta.logf("telegram: mcp startup error: %v", err)
	}

	ta.mu.Lock()
	ta.mcpManager = mgr
	// Propagate to any sessions created before MCP init.
	for _, svc := range ta.sessions {
		svc.SetMCPManager(mgr)
	}
	ta.mu.Unlock()

	names := mgr.ToolNames()
	if len(names) > 0 {
		ta.logf("telegram: mcp: %d tools from %d servers", len(names), len(cfg.MCPServers))
	}
}

func (ta *Adapter) applyDisabledToolsFromPrefs() {
	disabled := ta.Prefs.DisabledToolsSet()
	ta.mu.Lock()
	for _, svc := range ta.sessions {
		svc.SetDisabledTools(disabled)
	}
	ta.mu.Unlock()
	ta.logf("telegram: tools.disabled updated (%d disabled)", len(disabled))
}

func (ta *Adapter) applyXOAuthFromPrefs() {
	ta.mu.Lock()
	for _, svc := range ta.sessions {
		svc.SetXOAuth(
			ta.Prefs.XClientID,
			ta.Prefs.XClientSecret,
			ta.Prefs.XAccessToken,
			ta.Prefs.XRefreshToken,
			ta.Prefs.XTokenExpiry,
			func(accessToken, refreshToken, tokenExpiry string) error {
				ta.mu.Lock()
				defer ta.mu.Unlock()
				if ta.Prefs == nil {
					return nil
				}
				ta.Prefs.XAccessToken = accessToken
				ta.Prefs.XRefreshToken = refreshToken
				ta.Prefs.XTokenExpiry = tokenExpiry
				return config.SavePreferences(*ta.Prefs)
			},
		)
	}
	ta.mu.Unlock()
	ta.logf("telegram: X oauth updated")
}

func disabledToolsCSV(disabled map[string]bool) string {
	var out []string
	for name, off := range disabled {
		if off {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func (ta *Adapter) applyModelSpec(spec string) (providerName, modelID, keyWarning string) {
	currentProvider := ""
	if ta.Provider != nil && ta.Provider.Name() != "" {
		currentProvider = ta.Provider.Name()
	}
	newProviderName, newModelID := provider.ResolveProviderAndModel(spec, currentProvider)
	newProvider, err := provider.GetProvider(newProviderName)
	if err != nil {
		ta.logf("telegram: provider resolve error for %q: %v", spec, err)
		return currentProvider, ta.ModelID, ""
	}

	newKey, keyErr := config.LoadProviderAPIKey(*ta.Prefs, newProviderName)
	if keyErr != nil && newProviderName != "ollama" {
		newKey = ""
		keyWarning = fmt.Sprintf("No API key set for provider '%s'. Use /config set %s.api_key <key>", newProviderName, newProviderName)
	}

	ta.mu.Lock()
	ta.Provider = newProvider
	ta.ModelID = newModelID
	ta.ModelLabel = spec
	ta.APIKey = newKey
	for _, svc := range ta.sessions {
		svc.SetModel(spec, newModelID)
		svc.SetProvider(newProvider, newKey)
	}
	ta.mu.Unlock()

	ta.logf("telegram: model set provider=%s model=%s", newProviderName, newModelID)
	return newProviderName, newModelID, keyWarning
}

func (ta *Adapter) runAgent(chatID int64, svc *agent.Service, userText string) {
	var sentMsgID int
	var textBuf strings.Builder
	var lastEdit time.Time
	var editMu sync.Mutex

	// Immediate feedback so users know work has started before first delta/tool event.
	if sent, err := ta.bot.Send(tgbotapi.NewMessage(chatID, "Thinking...")); err == nil {
		sentMsgID = sent.MessageID
		// Allow first delta update to edit immediately.
		lastEdit = time.Now().Add(-EditInterval)
	}

	onEvent := func(evt agent.Event) {
		editMu.Lock()
		defer editMu.Unlock()

		switch evt.Kind {
		case agent.EventDelta:
			textBuf.WriteString(evt.DeltaText)

			// Send or edit the message at intervals
			if sentMsgID == 0 {
				// Send first message
				current := textBuf.String()
				if len(current) > MaxMessageLen {
					current = current[:MaxMessageLen]
				}
				reply := tgbotapi.NewMessage(chatID, current)
				sent, err := ta.bot.Send(reply)
				if err == nil {
					sentMsgID = sent.MessageID
					lastEdit = time.Now()
				}
			} else if time.Since(lastEdit) >= EditInterval {
				current := textBuf.String()
				if len(current) > MaxMessageLen {
					current = current[:MaxMessageLen]
				}
				edit := tgbotapi.NewEditMessageText(chatID, sentMsgID, current)
				ta.bot.Send(edit)
				lastEdit = time.Now()
			}

		case agent.EventToolStart:
			// Optionally notify about tool usage
			status := fmt.Sprintf("Running %s...", evt.ToolName)
			if sentMsgID == 0 {
				reply := tgbotapi.NewMessage(chatID, status)
				sent, err := ta.bot.Send(reply)
				if err == nil {
					sentMsgID = sent.MessageID
					lastEdit = time.Now()
				}
			}

		case agent.EventStreamDone:
			// Intermediate update -- plain text only (HTML applied at turn end)
			if sentMsgID != 0 && textBuf.Len() > 0 {
				ta.sendOrSplit(chatID, &sentMsgID, StripANSI(textBuf.String()), "")
				lastEdit = time.Now()
			}

		case agent.EventTurnDone:
			// Final update: send complete text with HTML formatting
			finalText := StripANSI(textBuf.String())
			if finalText == "" {
				finalText = "(No response)"
			}
			htmlText := MarkdownToTelegramHTML(finalText)
			if !ta.sendOrSplit(chatID, &sentMsgID, htmlText, tgbotapi.ModeHTML) {
				// Fallback to plain text if Telegram rejects the HTML
				ta.sendOrSplit(chatID, &sentMsgID, finalText, "")
			}

		case agent.EventRetrying:
			status := evt.RetryMessage
			if sentMsgID != 0 {
				edit := tgbotapi.NewEditMessageText(chatID, sentMsgID, status)
				ta.bot.Send(edit)
			} else {
				reply := tgbotapi.NewMessage(chatID, status)
				sent, err := ta.bot.Send(reply)
				if err == nil {
					sentMsgID = sent.MessageID
					lastEdit = time.Now()
				}
			}

		case agent.EventError:
			ta.logf("telegram: agent error: %v", evt.Err)
			errText := ""
			if evt.Err != nil {
				errText = strings.ToLower(strings.TrimSpace(evt.Err.Error()))
			}
			userMsg := "Error: " + sanitizeError(evt.Err)
			if strings.Contains(errText, "agent is already running") {
				userMsg = "Still processing your previous message. Please wait."
			}
			reply := tgbotapi.NewMessage(chatID, userMsg)
			ta.bot.Send(reply)

		case agent.EventAskUser:
			ta.mu.Lock()
			ta.askState[chatID] = evt.AskResponse
			ta.mu.Unlock()
			reply := tgbotapi.NewMessage(chatID, "Question: "+evt.AskPrompt)
			ta.bot.Send(reply)

		case agent.EventCompacted:
			reply := tgbotapi.NewMessage(chatID, "Context compacted to stay within limits.")
			ta.bot.Send(reply)
		}
	}

	svc.Submit(userText, onEvent)
}

// sendOrSplit sends a final message, splitting if it exceeds Telegram's limit.
// If a message was already sent (sentMsgID != 0), it edits the first part and
// sends additional messages for overflow. parseMode can be tgbotapi.ModeHTML
// or "" for plain text. Returns true if the first message was sent/edited
// successfully, false if Telegram rejected it (e.g. bad HTML).
func (ta *Adapter) sendOrSplit(chatID int64, sentMsgID *int, text, parseMode string) bool {
	parts := SplitMessage(text, MaxMessageLen)
	if parseMode == tgbotapi.ModeHTML {
		parts = SplitHTMLMessage(text, MaxMessageLen)
	}
	if len(parts) == 0 {
		return true
	}

	for i, part := range parts {
		if i == 0 && *sentMsgID != 0 {
			// Edit existing message
			edit := tgbotapi.NewEditMessageText(chatID, *sentMsgID, part)
			if parseMode != "" {
				edit.ParseMode = parseMode
			}
			_, err := ta.bot.Send(edit)
			if err != nil && i == 0 {
				return false
			}
		} else if i == 0 {
			// Send new message
			reply := tgbotapi.NewMessage(chatID, part)
			if parseMode != "" {
				reply.ParseMode = parseMode
			}
			sent, err := ta.bot.Send(reply)
			if err != nil {
				return false
			}
			*sentMsgID = sent.MessageID
		} else {
			// Send continuation
			reply := tgbotapi.NewMessage(chatID, part)
			if parseMode != "" {
				reply.ParseMode = parseMode
			}
			ta.bot.Send(reply)
		}
	}
	return true
}

// SplitMessage splits text into chunks of at most maxLen bytes.
// It tries to split at newline boundaries when possible.
func SplitMessage(text string, maxLen int) []string {
	return splitMessageInternal(text, maxLen, false)
}

// SplitHTMLMessage splits HTML text into chunks while avoiding splits inside
// tags/entities that could make Telegram reject parse_mode=HTML.
func SplitHTMLMessage(text string, maxLen int) []string {
	return splitMessageInternal(text, maxLen, true)
}

func splitMessageInternal(text string, maxLen int, htmlAware bool) []string {
	if maxLen <= 0 {
		maxLen = MaxMessageLen
	}
	if len(text) <= maxLen {
		return []string{text}
	}

	var parts []string
	remaining := text
	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			parts = append(parts, remaining)
			break
		}

		// Try to split at last newline within maxLen
		chunk := remaining[:maxLen]
		splitIdx := strings.LastIndex(chunk, "\n")
		if splitIdx < maxLen/2 {
			// No good newline boundary; hard split
			splitIdx = maxLen
		} else {
			splitIdx++ // include the newline
		}
		splitIdx = safeSplitIndex(remaining, splitIdx, maxLen/2, htmlAware)

		parts = append(parts, remaining[:splitIdx])
		remaining = remaining[splitIdx:]
	}
	return parts
}

func safeSplitIndex(text string, candidate int, min int, htmlAware bool) int {
	if candidate <= 0 {
		return 1
	}
	if candidate > len(text) {
		candidate = len(text)
	}
	if min < 1 {
		min = 1
	}
	idx := candidate
	for idx > min {
		prefix := text[:idx]
		if !utf8.ValidString(prefix) {
			idx--
			continue
		}
		if htmlAware && !isSafeHTMLBoundary(prefix) {
			idx--
			continue
		}
		return idx
	}
	return candidate
}

func isSafeHTMLBoundary(prefix string) bool {
	// Avoid splitting inside tags: "...<a href"
	lastLT := strings.LastIndex(prefix, "<")
	lastGT := strings.LastIndex(prefix, ">")
	if lastLT > lastGT {
		return false
	}

	// Avoid splitting inside entities: "...&amp"
	lastAmp := strings.LastIndex(prefix, "&")
	lastSemi := strings.LastIndex(prefix, ";")
	if lastAmp > lastSemi {
		return false
	}
	return true
}
