package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/tools"
)

// SlashCommands lists the available slash commands.
var SlashCommands = []string{
	"/branch", "/clear", "/config", "/continue", "/exit", "/help",
	"/new", "/quit", "/redo", "/refresh", "/rename", "/resume", "/schedule", "/sessions", "/telegram", "/tools", "/tweet", "/undo", "/x",
}

// ConfigSubcommands lists the available /config subcommands.
var ConfigSubcommands = []string{"messaging", "models", "reset", "set", "show", "theme", "tools"}
var ToolSubcommands = []string{"list", "enable", "disable", "toggle", "profile"}
var ToolProfiles = []string{"safe", "coder", "research"}
var ScheduleSubcommands = []string{"add", "list", "cancel"}
var XSubcommands = []string{"auth", "status", "logout"}

// ConfigKeys lists the available /config set keys.
var ConfigKeys = []string{
	"anthropic.api_key", "brave.api_key", "fireworks.api_key", "footer.cost", "footer.cwd",
	"footer.keybindings", "footer.session", "footer.tokens", "google.api_key",
	"grok.api_key", "mistral.api_key", "model", "ollama.url", "openai.api_key",
	"scheduler.allowed_tools",
	"x.client_id", "x.client_secret", "x.access_token", "x.refresh_token", "x.token_expiry", "x.redirect_url",
	"zai.api_key",
	"tools.disabled",
	"telegram.allowed_ids", "telegram.bot_token",
}

// ModelAliasNames returns the sorted list of model alias names.
func ModelAliasNames() []string {
	names := make([]string, 0, len(provider.ModelAliases))
	for k := range provider.ModelAliases {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

// ComputeCompletions returns full-input completion candidates for the given
// input string. extraModelIDs are additional model identifiers (e.g. from the
// API) to include when completing /model arguments.
func ComputeCompletions(input string, extraModelIDs []string) []string {
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	fields := strings.Fields(input)
	if len(fields) == 0 {
		return FilterByPrefix(SlashCommands, "/", "")
	}

	cmd := strings.ToLower(fields[0])

	// Still typing the command name (no space after it yet).
	if len(fields) == 1 && !strings.HasSuffix(input, " ") {
		return FilterByPrefix(SlashCommands, "", cmd)
	}

	// Command is complete -- dispatch on subcommand context.
	switch cmd {
	case "/config":
		if len(fields) == 1 || (len(fields) == 2 && !strings.HasSuffix(input, " ")) {
			partial := ""
			if len(fields) >= 2 {
				partial = strings.ToLower(fields[1])
			}
			return FilterByPrefix(ConfigSubcommands, "/config ", partial)
		}
		sub := strings.ToLower(fields[1])
		if sub == "set" {
			// /config set <key> — complete key names
			if len(fields) <= 3 && !(len(fields) == 3 && strings.HasSuffix(input, " ")) {
				partial := ""
				if len(fields) >= 3 {
					partial = strings.ToLower(fields[2])
				}
				return FilterByPrefix(ConfigKeys, "/config set ", partial)
			}
			// /config set model <value> — complete model names
			if len(fields) >= 3 && strings.ToLower(fields[2]) == "model" {
				partial := ""
				if len(fields) >= 4 {
					partial = strings.ToLower(fields[3])
				}
				candidates := ModelAliasNames()
				seen := make(map[string]bool, len(candidates))
				for _, c := range candidates {
					seen[c] = true
				}
				for _, id := range extraModelIDs {
					if !seen[id] {
						candidates = append(candidates, id)
					}
				}
				return FilterByPrefix(candidates, "/config set model ", partial)
			}
		}
		return nil

	case "/continue", "/resume":
		// Could add session ID completion in the future.
		return nil
	case "/tools":
		if len(fields) == 1 || (len(fields) == 2 && !strings.HasSuffix(input, " ")) {
			partial := ""
			if len(fields) >= 2 {
				partial = strings.ToLower(fields[1])
			}
			return FilterByPrefix(ToolSubcommands, "/tools ", partial)
		}
		sub := strings.ToLower(fields[1])
		switch sub {
		case "enable", "disable", "toggle":
			if len(fields) <= 3 && !(len(fields) == 3 && strings.HasSuffix(input, " ")) {
				partial := ""
				if len(fields) >= 3 {
					partial = strings.ToLower(fields[2])
				}
				names := tools.ToolNames()
				candidates := make([]string, 0, len(names))
				for _, n := range names {
					candidates = append(candidates, tools.ToolDisplayName(n))
				}
				return FilterByPrefix(candidates, "/tools "+sub+" ", partial)
			}
		case "profile":
			partial := ""
			if len(fields) >= 3 {
				partial = strings.ToLower(fields[2])
			}
			return FilterByPrefix(ToolProfiles, "/tools profile ", partial)
		}
		return nil
	case "/schedule":
		if len(fields) == 1 || (len(fields) == 2 && !strings.HasSuffix(input, " ")) {
			partial := ""
			if len(fields) >= 2 {
				partial = strings.ToLower(fields[1])
			}
			return FilterByPrefix(ScheduleSubcommands, "/schedule ", partial)
		}
		sub := strings.ToLower(fields[1])
		if sub == "add" {
			if len(fields) <= 3 && !(len(fields) == 3 && strings.HasSuffix(input, " ")) {
				partial := ""
				if len(fields) >= 3 {
					partial = strings.ToLower(fields[2])
				}
				names := tools.ToolNames()
				candidates := make([]string, 0, len(names))
				for _, n := range names {
					candidates = append(candidates, tools.ToolDisplayName(n))
				}
				return FilterByPrefix(candidates, "/schedule add ", partial)
			}
		}
		return nil
	case "/x":
		partial := ""
		if len(fields) >= 2 {
			partial = strings.ToLower(fields[1])
		}
		return FilterByPrefix(XSubcommands, "/x ", partial)
	}

	return nil
}

// FilterByPrefix returns candidates that start with partial, each prefixed
// with the given prefix string. If partial is empty, all candidates match.
func FilterByPrefix(candidates []string, prefix, partial string) []string {
	var result []string
	lower := strings.ToLower(partial)
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c), lower) {
			result = append(result, prefix+c)
		}
	}
	return result
}

// CommandExpectsArgs returns true if the completed command should have a
// trailing space appended (rather than being submitted) because it accepts
// an argument.
func CommandExpectsArgs(completion string) bool {
	fields := strings.Fields(completion)
	if len(fields) == 0 {
		return false
	}
	cmd := strings.ToLower(fields[0])
	switch cmd {
	case "/continue", "/resume", "/rename":
		return len(fields) == 1
	case "/tweet":
		return len(fields) == 1
	case "/schedule":
		if len(fields) == 1 {
			return true
		}
		sub := strings.ToLower(fields[1])
		return (sub == "add" || sub == "cancel") && len(fields) == 2
	case "/x":
		return len(fields) == 1
	case "/tools":
		if len(fields) == 1 {
			return false
		}
		sub := strings.ToLower(fields[1])
		return (sub == "enable" || sub == "disable" || sub == "toggle" || sub == "profile") && len(fields) == 2
	case "/config":
		if len(fields) == 1 {
			return true
		}
		sub := strings.ToLower(fields[1])
		if sub == "set" {
			// /config set → expects key; /config set model → expects value
			if len(fields) == 2 {
				return true
			}
			return strings.ToLower(fields[2]) == "model" && len(fields) == 3
		}
		return false
	}
	return false
}

// RenderCompletionMenu renders up to maxVisible completion items as a
// vertical menu. The selected item is highlighted.
func RenderCompletionMenu(completions []string, selectedIdx, width int) string {
	const maxVisible = 8
	n := len(completions)
	if n == 0 {
		return ""
	}

	var b strings.Builder
	visible := min(n, maxVisible)
	for i := 0; i < visible; i++ {
		label := completions[i]
		if len(label) > width-4 {
			label = label[:width-4]
		}
		if i == selectedIdx {
			b.WriteString(CompletionSelStyle.Render(" " + label + " "))
		} else {
			b.WriteString(CompletionStyle.Render(" " + label + " "))
		}
		b.WriteString("\n")
	}
	if n > maxVisible {
		more := fmt.Sprintf(" ... and %d more", n-maxVisible)
		b.WriteString(CompletionStyle.Render(more))
		b.WriteString("\n")
	}
	return b.String()
}
