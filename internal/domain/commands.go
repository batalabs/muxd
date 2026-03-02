package domain

// CommandDef describes a slash command available to the user.
type CommandDef struct {
	Name        string
	Description string
	Group       string // display group for /help
	TUIOnly     bool
}

// CommandDefs is the single source of truth for all slash commands.
var CommandDefs = []CommandDef{
	// Session
	{Name: "/new", Description: "start a new session", Group: "session"},
	{Name: "/sessions", Description: "list and switch sessions", Group: "session"},
	{Name: "/continue", Description: "resume a session by ID", Group: "session"},
	{Name: "/branch", Description: "fork conversation at current point", Group: "session"},
	{Name: "/rename", Description: "rename current session", Group: "session"},
	// Editing
	{Name: "/undo", Description: "undo last agent turn", Group: "editing", TUIOnly: true},
	{Name: "/redo", Description: "redo last undone turn", Group: "editing", TUIOnly: true},
	{Name: "/sh", Description: "drop into muxd shell", Group: "editing", TUIOnly: true},
	// Config & tools
	{Name: "/config", Description: "show/set preferences", Group: "config"},
	{Name: "/tools", Description: "picker + enable/disable/profile tools", Group: "config"},
	{Name: "/emoji", Description: "pick a footer emoji", Group: "config", TUIOnly: true},
	{Name: "/nodes", Description: "list and select hub nodes", Group: "config", TUIOnly: true},
	{Name: "/qr", Description: "show QR code for mobile app connection", Group: "config", TUIOnly: true},
	{Name: "/schedule", Description: "manage generic scheduled tool jobs", Group: "config"},
	{Name: "/remember", Description: "save a fact to project memory", Group: "config"},
	// General
	{Name: "/help", Description: "show this help", Group: "general"},
	{Name: "/clear", Description: "clear chat", Group: "general", TUIOnly: true},
	{Name: "/refresh", Description: "reload current session messages", Group: "general", TUIOnly: true},
	{Name: "/exit", Description: "quit muxd", Group: "general", TUIOnly: true},
}

// CommandHelp returns the list of commands visible in the TUI.
func CommandHelp() []CommandDef {
	return append([]CommandDef(nil), CommandDefs...)
}

// CommandGroups defines the display order and labels for help groups.
var CommandGroups = []struct {
	Key   string
	Label string
}{
	{"session", "Sessions"},
	{"editing", "Editing"},
	{"config", "Config"},
	{"general", "General"},
}
