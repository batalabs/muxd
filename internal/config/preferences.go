package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Preferences holds user-configurable display and behavior settings.
// Persisted to ~/.config/muxd/config.json.
type Preferences struct {
	FooterTokens      bool   `json:"footer_tokens"`
	FooterCost        bool   `json:"footer_cost"`
	FooterCwd         bool   `json:"footer_cwd"`
	FooterSession     bool   `json:"footer_session"`
	FooterKeybindings bool   `json:"footer_keybindings"`
	FooterEmoji       string `json:"footer_emoji,omitempty"`
	Model             string `json:"model"`
	ModelCompact      string `json:"model_compact,omitempty"`
	ModelTitle        string `json:"model_title,omitempty"`
	ModelTags         string `json:"model_tags,omitempty"`

	// Provider and API keys
	Provider              string `json:"provider,omitempty"`
	AnthropicAPIKey       string `json:"anthropic_api_key,omitempty"`
	ZAIAPIKey             string `json:"zai_api_key,omitempty"`
	GrokAPIKey            string `json:"grok_api_key,omitempty"`
	MistralAPIKey         string `json:"mistral_api_key,omitempty"`
	OpenAIAPIKey          string `json:"openai_api_key,omitempty"`
	GoogleAPIKey          string `json:"google_api_key,omitempty"`
	FireworksAPIKey       string `json:"fireworks_api_key,omitempty"`
	BraveAPIKey           string `json:"brave_api_key,omitempty"`
	TextbeltAPIKey        string `json:"textbelt_api_key,omitempty"`
	SchedulerAllowedTools string `json:"scheduler_allowed_tools,omitempty"`
	ToolsDisabled         string `json:"tools_disabled,omitempty"`
	OllamaURL             string `json:"ollama_url,omitempty"`

	// Daemon settings
	DaemonBindAddress string `json:"daemon_bind_address,omitempty"`
	DaemonAuthToken   string `json:"daemon_auth_token,omitempty"`

	// Hub settings
	HubBindAddress string `json:"hub_bind_address,omitempty"`
	HubAuthToken   string `json:"hub_auth_token,omitempty"`
	HubURL         string `json:"hub_url,omitempty"`
	HubNodeToken   string `json:"hub_node_token,omitempty"`
	HubNodeID      string `json:"hub_node_id,omitempty"`
	HubNodeName    string `json:"hub_node_name,omitempty"`
}

// PrefEntry holds a single key-value preference entry for display.
type PrefEntry struct {
	Key   string
	Value string
}

// ConfigGroup holds a named group of preference entries for display.
type ConfigGroup struct {
	Name    string
	Entries []PrefEntry
}

// ConfigGroupDef defines a single group with a name and its keys.
type ConfigGroupDef struct {
	Name string
	Keys []string
}

// ConfigGroupDefs defines the preference key groupings and their display order.
var ConfigGroupDefs = []ConfigGroupDef{
	{
		Name: "models",
		Keys: []string{"model", "model.compact", "model.title", "model.tags", "anthropic.api_key", "zai.api_key", "grok.api_key", "mistral.api_key", "openai.api_key", "google.api_key", "fireworks.api_key", "ollama.url"},
	},
	{
		Name: "tools",
		Keys: []string{"brave.api_key", "textbelt.api_key", "scheduler.allowed_tools"},
	},
	{
		Name: "daemon",
		Keys: []string{"daemon.bind_address", "daemon.auth_token"},
	},
	{
		Name: "hub",
		Keys: []string{"hub.bind_address", "hub.auth_token"},
	},
	{
		Name: "node",
		Keys: []string{"hub.url", "hub.node_token", "hub.node_id", "hub.node_name"},
	},
	{
		Name: "theme",
		Keys: []string{"footer.tokens", "footer.cost", "footer.cwd", "footer.session", "footer.keybindings", "footer.emoji"},
	},
}

// ConfigGroupNames returns the list of valid group names.
func ConfigGroupNames() []string {
	names := make([]string, len(ConfigGroupDefs))
	for i, g := range ConfigGroupDefs {
		names[i] = g.Name
	}
	return names
}

// ValidConfigKeys returns all config keys accepted by Set().
func ValidConfigKeys() []string {
	var keys []string
	for _, g := range ConfigGroupDefs {
		keys = append(keys, g.Keys...)
	}
	return keys
}

// DefaultPreferences returns the default set of preferences.
func DefaultPreferences() Preferences {
	return Preferences{
		FooterTokens:      true,
		FooterCost:        true,
		FooterCwd:         true,
		FooterSession:     true,
		FooterKeybindings: true,
		Model:             "",
		Provider:          "",
		OllamaURL:         "",
	}
}

// LoadPreferences reads preferences from ~/.config/muxd/config.json.
// If preferences.json also exists (legacy), it merges both files — values
// from preferences.json take priority — then saves the merged result to
// config.json and removes the old file.
func LoadPreferences() Preferences {
	dir := ConfigDir()
	if dir == "" {
		return DefaultPreferences()
	}

	configPath := filepath.Join(dir, "config.json")
	legacyPath := filepath.Join(dir, "preferences.json")

	p := DefaultPreferences()

	// Load config.json if it exists
	configLoaded := false
	if data, err := os.ReadFile(configPath); err == nil {
		// Strip UTF-8 BOM that Windows editors (e.g. Notepad) may add
		data = stripBOM(data)
		if err := json.Unmarshal(data, &p); err != nil {
			fmt.Fprintf(os.Stderr, "config: parse %s: %v\n", configPath, err)
		} else {
			configLoaded = true
		}
		warnInsecurePermissions(configPath)
	}

	// Only sanitize and re-save if we successfully loaded the config.
	// This prevents overwriting the user's file with defaults on parse errors.
	if configLoaded && sanitizePreferences(&p) {
		if err := SavePreferences(p); err != nil {
			fmt.Fprintf(os.Stderr, "config: save sanitized config: %v\n", err)
		}
	}

	// If legacy preferences.json exists, merge it (values win over config.json)
	// then delete the legacy file
	if data, err := os.ReadFile(legacyPath); err == nil {
		data = stripBOM(data)
		legacy := DefaultPreferences()
		if json.Unmarshal(data, &legacy) == nil {
			mergePreferences(&p, &legacy)
			if err := SavePreferences(p); err != nil {
				fmt.Fprintf(os.Stderr, "config: save merged preferences: %v\n", err)
			}
			if err := os.Remove(legacyPath); err != nil {
				fmt.Fprintf(os.Stderr, "config: remove legacy %s: %v\n", legacyPath, err)
			}
		}
	}

	return p
}

// mergePreferences copies non-empty values from src into dst.
func mergePreferences(dst, src *Preferences) {
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.ModelCompact != "" {
		dst.ModelCompact = src.ModelCompact
	}
	if src.ModelTitle != "" {
		dst.ModelTitle = src.ModelTitle
	}
	if src.ModelTags != "" {
		dst.ModelTags = src.ModelTags
	}
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.AnthropicAPIKey != "" {
		dst.AnthropicAPIKey = src.AnthropicAPIKey
	}
	if src.ZAIAPIKey != "" {
		dst.ZAIAPIKey = src.ZAIAPIKey
	}
	if src.OpenAIAPIKey != "" {
		dst.OpenAIAPIKey = src.OpenAIAPIKey
	}
	if src.MistralAPIKey != "" {
		dst.MistralAPIKey = src.MistralAPIKey
	}
	if src.GrokAPIKey != "" {
		dst.GrokAPIKey = src.GrokAPIKey
	}
	if src.GoogleAPIKey != "" {
		dst.GoogleAPIKey = src.GoogleAPIKey
	}
	if src.FireworksAPIKey != "" {
		dst.FireworksAPIKey = src.FireworksAPIKey
	}
	if src.BraveAPIKey != "" {
		dst.BraveAPIKey = src.BraveAPIKey
	}
	if src.TextbeltAPIKey != "" {
		dst.TextbeltAPIKey = src.TextbeltAPIKey
	}
	if src.SchedulerAllowedTools != "" {
		dst.SchedulerAllowedTools = src.SchedulerAllowedTools
	}
	if src.ToolsDisabled != "" {
		dst.ToolsDisabled = src.ToolsDisabled
	}
	if src.OllamaURL != "" {
		dst.OllamaURL = src.OllamaURL
	}
	if src.DaemonBindAddress != "" {
		dst.DaemonBindAddress = src.DaemonBindAddress
	}
	if src.DaemonAuthToken != "" {
		dst.DaemonAuthToken = src.DaemonAuthToken
	}
	if src.HubBindAddress != "" {
		dst.HubBindAddress = src.HubBindAddress
	}
	if src.HubAuthToken != "" {
		dst.HubAuthToken = src.HubAuthToken
	}
	if src.HubURL != "" {
		dst.HubURL = src.HubURL
	}
	if src.HubNodeToken != "" {
		dst.HubNodeToken = src.HubNodeToken
	}
	if src.HubNodeID != "" {
		dst.HubNodeID = src.HubNodeID
	}
	if src.HubNodeName != "" {
		dst.HubNodeName = src.HubNodeName
	}
	// Booleans: copy from src (they represent the user's last settings)
	dst.FooterTokens = src.FooterTokens
	dst.FooterCost = src.FooterCost
	dst.FooterCwd = src.FooterCwd
	dst.FooterSession = src.FooterSession
	dst.FooterKeybindings = src.FooterKeybindings
	if src.FooterEmoji != "" {
		dst.FooterEmoji = src.FooterEmoji
	}
}

// SavePreferences writes preferences to ~/.config/muxd/config.json.
func SavePreferences(p Preferences) error {
	dir := ConfigDir()
	if dir == "" {
		return fmt.Errorf("could not determine config directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)
}

// stripBOM removes a UTF-8 BOM prefix if present. Windows editors like
// Notepad may add a BOM which breaks JSON parsing.
func stripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

// warnInsecurePermissions prints a warning to stderr if the config file is
// readable by group or others. On Windows, file permission bits don't map
// to ACLs, so the check is skipped.
func warnInsecurePermissions(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %s is readable by others (mode %o). Run: chmod 600 %s\n",
			path, info.Mode().Perm(), path)
	}
}

// Grouped returns all preferences organized into named groups.
// Values are display-ready: API keys are masked, empty values show defaults.
func (p Preferences) Grouped() []ConfigGroup {
	all := p.entryMap()
	defaults := DefaultPreferences().entryMap()

	var groups []ConfigGroup
	for _, def := range ConfigGroupDefs {
		var entries []PrefEntry
		for _, key := range def.Keys {
			val := all[key]
			defVal := defaults[key]
			entries = append(entries, PrefEntry{
				Key:   key,
				Value: AnnotateValue(val, defVal),
			})
		}
		groups = append(groups, ConfigGroup{Name: def.Name, Entries: entries})
	}
	return groups
}

// GroupByName returns entries for a single config group, or nil if not found.
func (p Preferences) GroupByName(name string) *ConfigGroup {
	for _, g := range p.Grouped() {
		if g.Name == name {
			return &g
		}
	}
	return nil
}

// entryMap returns all preference entries as a key->value map.
func (p Preferences) entryMap() map[string]string {
	m := make(map[string]string)
	for _, e := range p.All() {
		m[e.Key] = e.Value
	}
	return m
}

// All returns all preference entries as a flat list.
func (p Preferences) All() []PrefEntry {
	return []PrefEntry{
		{"footer.tokens", strconv.FormatBool(p.FooterTokens)},
		{"footer.cost", strconv.FormatBool(p.FooterCost)},
		{"footer.cwd", strconv.FormatBool(p.FooterCwd)},
		{"footer.session", strconv.FormatBool(p.FooterSession)},
		{"footer.keybindings", strconv.FormatBool(p.FooterKeybindings)},
		{"footer.emoji", p.FooterEmoji},
		{"model", p.Model},
		{"model.compact", p.ModelCompact},
		{"model.title", p.ModelTitle},
		{"model.tags", p.ModelTags},
		{"anthropic.api_key", resolveKeyDisplay(p.AnthropicAPIKey, "ANTHROPIC_API_KEY")},
		{"zai.api_key", resolveKeyDisplay(p.ZAIAPIKey, "ZAI_API_KEY")},
		{"grok.api_key", resolveKeyDisplay(p.GrokAPIKey, "XAI_API_KEY")},
		{"mistral.api_key", resolveKeyDisplay(p.MistralAPIKey, "MISTRAL_API_KEY")},
		{"openai.api_key", resolveKeyDisplay(p.OpenAIAPIKey, "OPENAI_API_KEY")},
		{"google.api_key", resolveKeyDisplay(p.GoogleAPIKey, "GOOGLE_API_KEY")},
		{"fireworks.api_key", resolveKeyDisplay(p.FireworksAPIKey, "FIREWORKS_API_KEY")},
		{"brave.api_key", resolveKeyDisplay(p.BraveAPIKey, "BRAVE_SEARCH_API_KEY")},
		{"textbelt.api_key", MaskKey(p.TextbeltAPIKey)},
		{"scheduler.allowed_tools", p.SchedulerAllowedTools},
		{"tools.disabled", p.ToolsDisabled},
		{"ollama.url", p.OllamaURL},
		{"daemon.bind_address", p.DaemonBindAddress},
		{"daemon.auth_token", MaskKey(p.DaemonAuthToken)},
		{"hub.bind_address", p.HubBindAddress},
		{"hub.auth_token", MaskKey(p.HubAuthToken)},
		{"hub.url", p.HubURL},
		{"hub.node_token", MaskKey(p.HubNodeToken)},
		{"hub.node_id", p.HubNodeID},
		{"hub.node_name", p.HubNodeName},
	}
}

// Get returns the display value for a single preference key.
func (p Preferences) Get(key string) string {
	switch key {
	case "footer.tokens":
		return strconv.FormatBool(p.FooterTokens)
	case "footer.cost":
		return strconv.FormatBool(p.FooterCost)
	case "footer.cwd":
		return strconv.FormatBool(p.FooterCwd)
	case "footer.session":
		return strconv.FormatBool(p.FooterSession)
	case "footer.keybindings":
		return strconv.FormatBool(p.FooterKeybindings)
	case "footer.emoji":
		return p.FooterEmoji
	case "model":
		return p.Model
	case "model.compact":
		return p.ModelCompact
	case "model.title":
		return p.ModelTitle
	case "model.tags":
		return p.ModelTags
	case "anthropic.api_key":
		return MaskKey(p.AnthropicAPIKey)
	case "zai.api_key":
		return MaskKey(p.ZAIAPIKey)
	case "openai.api_key":
		return MaskKey(p.OpenAIAPIKey)
	case "mistral.api_key":
		return MaskKey(p.MistralAPIKey)
	case "grok.api_key":
		return MaskKey(p.GrokAPIKey)
	case "google.api_key":
		return MaskKey(p.GoogleAPIKey)
	case "fireworks.api_key":
		return MaskKey(p.FireworksAPIKey)
	case "brave.api_key":
		return MaskKey(p.BraveAPIKey)
	case "textbelt.api_key":
		return MaskKey(p.TextbeltAPIKey)
	case "scheduler.allowed_tools":
		return p.SchedulerAllowedTools
	case "tools.disabled":
		return p.ToolsDisabled
	case "ollama.url":
		return p.OllamaURL
	case "daemon.bind_address":
		return p.DaemonBindAddress
	case "daemon.auth_token":
		return MaskKey(p.DaemonAuthToken)
	case "hub.bind_address":
		return p.HubBindAddress
	case "hub.auth_token":
		return MaskKey(p.HubAuthToken)
	case "hub.url":
		return p.HubURL
	case "hub.node_token":
		return MaskKey(p.HubNodeToken)
	case "hub.node_id":
		return p.HubNodeID
	case "hub.node_name":
		return p.HubNodeName
	default:
		return ""
	}
}

// Set updates a single preference key to the given value.
func (p *Preferences) Set(key, value string) error {
	value = SanitizeValue(value)
	switch key {
	case "footer.tokens":
		b, err := ParseBoolish(value)
		if err != nil {
			return err
		}
		p.FooterTokens = b
	case "footer.cost":
		b, err := ParseBoolish(value)
		if err != nil {
			return err
		}
		p.FooterCost = b
	case "footer.cwd":
		b, err := ParseBoolish(value)
		if err != nil {
			return err
		}
		p.FooterCwd = b
	case "footer.session":
		b, err := ParseBoolish(value)
		if err != nil {
			return err
		}
		p.FooterSession = b
	case "footer.keybindings":
		b, err := ParseBoolish(value)
		if err != nil {
			return err
		}
		p.FooterKeybindings = b
	case "footer.emoji":
		p.FooterEmoji = ResolveEmoji(value)
	case "model":
		p.Model = value
	case "model.compact":
		p.ModelCompact = value
	case "model.title":
		p.ModelTitle = value
	case "model.tags":
		p.ModelTags = value
	case "anthropic.api_key":
		p.AnthropicAPIKey = value
	case "zai.api_key":
		p.ZAIAPIKey = value
	case "openai.api_key":
		p.OpenAIAPIKey = value
	case "mistral.api_key":
		p.MistralAPIKey = value
	case "grok.api_key":
		p.GrokAPIKey = value
	case "google.api_key":
		p.GoogleAPIKey = value
	case "fireworks.api_key":
		p.FireworksAPIKey = value
	case "brave.api_key":
		p.BraveAPIKey = value
	case "textbelt.api_key":
		p.TextbeltAPIKey = value
	case "scheduler.allowed_tools":
		p.SchedulerAllowedTools = value
	case "tools.disabled":
		p.ToolsDisabled = value
	case "ollama.url":
		p.OllamaURL = value
	case "daemon.bind_address":
		p.DaemonBindAddress = value
	case "daemon.auth_token":
		p.DaemonAuthToken = value
	case "hub.bind_address":
		p.HubBindAddress = value
	case "hub.auth_token":
		p.HubAuthToken = value
	case "hub.url":
		p.HubURL = value
	case "hub.node_token":
		p.HubNodeToken = value
	case "hub.node_id":
		p.HubNodeID = value
	case "hub.node_name":
		p.HubNodeName = value
	default:
		return fmt.Errorf("unknown key: %s", key)
	}
	return nil
}

// SanitizeValue strips null bytes, ASCII control characters (< 32 except
// \n and \t), and DEL (0x7F) from a string value and trims surrounding
// whitespace. API keys and secrets should never contain control characters —
// these typically sneak in through clipboard paste artifacts.
func SanitizeValue(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 32 && r != '\n' && r != '\t') || r == 0x7F {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}

// isSensitiveKey returns true for config keys whose values should be sanitized
// (API keys, secrets, tokens, client IDs — anything that may be pasted).
func isSensitiveKey(key string) bool {
	return strings.HasSuffix(key, ".api_key") ||
		strings.HasSuffix(key, ".api_secret") ||
		strings.HasSuffix(key, ".bearer_token") ||
		strings.HasSuffix(key, ".client_id") ||
		strings.HasSuffix(key, ".client_secret") ||
		strings.HasSuffix(key, ".access_token") ||
		strings.HasSuffix(key, ".refresh_token") ||
		strings.HasSuffix(key, ".bot_token")
}

// sanitizePreferences strips control characters from all string fields in
// an already-loaded Preferences struct. Returns true if any field was modified.
func sanitizePreferences(p *Preferences) bool {
	changed := false
	sanitize := func(s *string) {
		cleaned := SanitizeValue(*s)
		if cleaned != *s {
			*s = cleaned
			changed = true
		}
	}
	sanitize(&p.Model)
	sanitize(&p.ModelCompact)
	sanitize(&p.ModelTitle)
	sanitize(&p.ModelTags)
	sanitize(&p.Provider)
	sanitize(&p.AnthropicAPIKey)
	sanitize(&p.ZAIAPIKey)
	sanitize(&p.GrokAPIKey)
	sanitize(&p.MistralAPIKey)
	sanitize(&p.OpenAIAPIKey)
	sanitize(&p.GoogleAPIKey)
	sanitize(&p.FireworksAPIKey)
	sanitize(&p.BraveAPIKey)
	sanitize(&p.TextbeltAPIKey)
	sanitize(&p.SchedulerAllowedTools)
	sanitize(&p.ToolsDisabled)
	sanitize(&p.OllamaURL)
	sanitize(&p.DaemonBindAddress)
	sanitize(&p.DaemonAuthToken)
	sanitize(&p.HubBindAddress)
	sanitize(&p.HubAuthToken)
	sanitize(&p.HubURL)
	sanitize(&p.HubNodeToken)
	sanitize(&p.HubNodeID)
	sanitize(&p.HubNodeName)
	sanitize(&p.FooterEmoji)
	return changed
}

// emojiPresets maps short names to their Unicode emoji for footer.emoji config.
var emojiPresets = map[string]string{
	"devil":   "\U0001f608",
	"imp":     "\U0001f47f",
	"fire":    "\U0001f525",
	"rocket":  "\U0001f680",
	"star":    "\u2b50",
	"bolt":    "\u26a1",
	"heart":   "\u2764\ufe0f",
	"skull":   "\U0001f480",
	"ghost":   "\U0001f47b",
	"alien":   "\U0001f47e",
	"robot":   "\U0001f916",
	"diamond": "\U0001f48e",
	"snake":   "\U0001f40d",
	"cat":     "\U0001f431",
	"dog":     "\U0001f436",
	"none":    "",
	"off":     "",
}

// ResolveEmoji maps a preset name to its emoji, or returns the value as-is.
func ResolveEmoji(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if emoji, ok := emojiPresets[lower]; ok {
		return emoji
	}
	return value
}

// EmojiPresetNames returns sorted preset names for display.
func EmojiPresetNames() []string {
	names := make([]string, 0, len(emojiPresets))
	for k := range emojiPresets {
		if k != "off" && k != "none" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveKeyDisplay returns a masked key for display. If the preference is
// empty but the env var is set, shows the masked env value with "(from env)".
func resolveKeyDisplay(prefKey, envVar string) string {
	if prefKey != "" {
		return MaskKey(prefKey)
	}
	if envVal := strings.TrimSpace(os.Getenv(envVar)); envVal != "" {
		return MaskKey(envVal) + " (from env)"
	}
	return ""
}

// MaskKey masks an API key for display, showing only the last 4 characters.
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// ParseAllowedIDs parses a comma-separated list of int64 user IDs.
func ParseAllowedIDs(s string) ([]int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ParseBoolish parses a boolean-like string value.
func ParseBoolish(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "on", "yes", "1":
		return true, nil
	case "false", "off", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s (use true/false, on/off, yes/no)", s)
	}
}

// DisabledToolsSet parses tools.disabled into a normalized set.
// Format: comma-separated tool names (e.g. "sms_send,web_fetch").
func (p Preferences) DisabledToolsSet() map[string]bool {
	out := map[string]bool{}
	raw := strings.TrimSpace(p.ToolsDisabled)
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

// ScheduledAllowedToolsSet parses scheduler.allowed_tools into a normalized set.
// Empty value falls back to a safe default allowlist.
func (p Preferences) ScheduledAllowedToolsSet() map[string]bool {
	out := map[string]bool{}
	raw := strings.TrimSpace(p.SchedulerAllowedTools)
	if raw == "" {
		for _, name := range []string{
			"file_read", "grep", "list_files", "git_status",
			"web_search", "web_fetch", "todo_read",
		} {
			out[name] = true
		}
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

// AnnotateValue returns a display string for a config value.
// Shows "(not set)" for empty values, otherwise shows the raw value.
func AnnotateValue(value, defaultValue string) string {
	if value == "" {
		return "(not set)"
	}
	return value
}

// ConfigFilePath returns the absolute path to config.json.
func ConfigFilePath() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// PreferencesFilePath is an alias for ConfigFilePath (legacy name).
// Deprecated: use ConfigFilePath.
func PreferencesFilePath() string {
	return ConfigFilePath()
}

// ---------------------------------------------------------------------------
// Config actions — adapter-agnostic business logic
// ---------------------------------------------------------------------------

// ExecuteConfigAction handles /config subcommands and returns a plain-text
// response. The caller (TUI or hub) applies its own formatting.
func ExecuteConfigAction(prefs *Preferences, args []string) (string, error) {
	sub := "show"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "show":
		return FormatConfigGroups(prefs.Grouped()), nil

	case "models", "tools", "daemon", "hub", "node", "theme":
		group := prefs.GroupByName(sub)
		if group == nil {
			return "", fmt.Errorf("unknown config group: %s", sub)
		}
		return FormatConfigGroups([]ConfigGroup{*group}), nil

	case "set":
		if len(args) < 3 {
			return "", fmt.Errorf("usage: /config set <key> <value>")
		}
		key := args[1]
		value := args[2]
		if err := prefs.Set(key, value); err != nil {
			return "", err
		}
		if err := SavePreferences(*prefs); err != nil {
			return "", fmt.Errorf("failed to save: %w", err)
		}
		return fmt.Sprintf("Set %s = %s", key, prefs.Get(key)), nil

	case "reset":
		*prefs = DefaultPreferences()
		if err := SavePreferences(*prefs); err != nil {
			return "", fmt.Errorf("failed to save: %w", err)
		}
		return "Preferences reset to defaults.", nil

	default:
		return "", fmt.Errorf("usage: /config [show|models|tools|daemon|hub|node|theme|set <key> <value>|reset]")
	}
}

// FormatConfigGroups renders config groups as plain text (no ANSI styling).
func FormatConfigGroups(groups []ConfigGroup) string {
	var lines []string
	for i, g := range groups {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.ToUpper(g.Name[:1])+g.Name[1:]+":")
		for _, e := range g.Entries {
			lines = append(lines, fmt.Sprintf("  %-24s %s", e.Key, e.Value))
		}
	}
	lines = append(lines, "")
	lines = append(lines, "  Use /config set <key> <value> to change")
	return strings.Join(lines, "\n")
}
