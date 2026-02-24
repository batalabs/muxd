package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMaskKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty key", "", ""},
		{"short key", "abc", "****"},
		{"exactly 4 chars", "abcd", "****"},
		{"normal key", "sk-ant-api03-abc123xyz", "****3xyz"},
		{"long key", "sk-ant-api03-very-long-key-here-1234", "****1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskKey(tt.key)
			if got != tt.want {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseAllowedIDs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIDs []int64
		wantErr bool
	}{
		{"empty string", "", nil, false},
		{"whitespace only", "  ", nil, false},
		{"single ID", "123", []int64{123}, false},
		{"multiple IDs", "123,456,789", []int64{123, 456, 789}, false},
		{"IDs with spaces", " 123 , 456 , 789 ", []int64{123, 456, 789}, false},
		{"trailing comma", "123,456,", []int64{123, 456}, false},
		{"invalid ID", "123,abc,456", nil, true},
		{"negative ID", "123,-456", []int64{123, -456}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, err := ParseAllowedIDs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAllowedIDs(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAllowedIDs(%q) unexpected error: %v", tt.input, err)
			}
			if len(ids) != len(tt.wantIDs) {
				t.Fatalf("ParseAllowedIDs(%q) got %d IDs, want %d", tt.input, len(ids), len(tt.wantIDs))
			}
			for i, id := range ids {
				if id != tt.wantIDs[i] {
					t.Errorf("ParseAllowedIDs(%q)[%d] = %d, want %d", tt.input, i, id, tt.wantIDs[i])
				}
			}
		})
	}
}

func TestPreferences_SetGet_newKeys(t *testing.T) {
	tests := []struct {
		key   string
		value string
		want  string
	}{
		{"anthropic.api_key", "sk-ant-test1234", "****1234"},
		{"zai.api_key", "zai-secret-7777", "****7777"},
		{"grok.api_key", "xai-secret-1111", "****1111"},
		{"mistral.api_key", "mistral-test-9999", "****9999"},
		{"openai.api_key", "sk-openai-test5678", "****5678"},
		{"google.api_key", "AIza-test-key-9012", "****9012"},
		{"ollama.url", "http://localhost:11434", "http://localhost:11434"},
		{"telegram.bot_token", "123456:ABC-DEF", "****-DEF"},
		{"telegram.allowed_ids", "123,456", "123,456"},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			p := DefaultPreferences()
			if err := p.Set(tt.key, tt.value); err != nil {
				t.Fatalf("Set(%q, %q) error: %v", tt.key, tt.value, err)
			}
			got := p.Get(tt.key)
			if got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestPreferences_Set_invalidAllowedIDs(t *testing.T) {
	p := DefaultPreferences()
	err := p.Set("telegram.allowed_ids", "abc,def")
	if err == nil {
		t.Fatal("expected error for non-numeric allowed IDs")
	}
}

func TestPreferences_All_masksKeys(t *testing.T) {
	p := DefaultPreferences()
	p.AnthropicAPIKey = "sk-ant-api03-long-key-1234"
	p.OpenAIAPIKey = "sk-openai-key-5678"

	entries := p.All()
	for _, e := range entries {
		switch e.Key {
		case "anthropic.api_key":
			if e.Value != "****1234" {
				t.Errorf("anthropic.api_key not masked: %q", e.Value)
			}
		case "openai.api_key":
			if e.Value != "****5678" {
				t.Errorf("openai.api_key not masked: %q", e.Value)
			}
		}
	}
}

func TestPreferences_Grouped(t *testing.T) {
	p := DefaultPreferences()
	p.AnthropicAPIKey = "sk-ant-api03-long-key-1234"
	p.TelegramBotToken = "123456:ABC-DEF"

	groups := p.Grouped()
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}

	// Verify group names
	wantNames := []string{"models", "tools", "messaging", "theme"}
	for i, g := range groups {
		if g.Name != wantNames[i] {
			t.Errorf("group %d name = %q, want %q", i, g.Name, wantNames[i])
		}
	}

	// Models group should show raw values, no "(default)" annotations
	models := groups[0]
	for _, e := range models.Entries {
		if e.Key == "anthropic.api_key" {
			if e.Value != "****1234" {
				t.Errorf("anthropic.api_key not masked in group: %q", e.Value)
			}
		}
		if e.Key == "openai.api_key" {
			if e.Value != "(not set)" {
				t.Errorf("openai.api_key = %q, want %q", e.Value, "(not set)")
			}
		}
	}

	// Messaging group should have masked bot token
	messaging := groups[2]
	for _, e := range messaging.Entries {
		if e.Key == "telegram.bot_token" {
			if e.Value != "****-DEF" {
				t.Errorf("telegram.bot_token not masked in group: %q", e.Value)
			}
		}
	}

	// Theme group: all booleans should show "true"
	theme := groups[3]
	for _, e := range theme.Entries {
		if e.Value != "true" {
			t.Errorf("theme key %q = %q, want %q", e.Key, e.Value, "true")
		}
	}
}

func TestPreferences_GroupByName(t *testing.T) {
	p := DefaultPreferences()

	t.Run("valid group", func(t *testing.T) {
		g := p.GroupByName("models")
		if g == nil {
			t.Fatal("expected non-nil group for 'models'")
		}
		if g.Name != "models" {
			t.Errorf("group name = %q, want %q", g.Name, "models")
		}
	})

	t.Run("invalid group returns nil", func(t *testing.T) {
		g := p.GroupByName("nonexistent")
		if g != nil {
			t.Error("expected nil for nonexistent group")
		}
	})
}

func TestAnnotateValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		defValue string
		want     string
	}{
		{"empty with no default", "", "", "(not set)"},
		{"empty with default", "", "anthropic", "(not set)"},
		{"has value", "anthropic", "anthropic", "anthropic"},
		{"differs from default", "openai", "anthropic", "openai"},
		{"non-empty no default", "custom", "", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnnotateValue(tt.value, tt.defValue)
			if got != tt.want {
				t.Errorf("AnnotateValue(%q, %q) = %q, want %q", tt.value, tt.defValue, got, tt.want)
			}
		})
	}
}

func TestLoadProviderAPIKey_envOverride(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.AnthropicAPIKey = "from-prefs"

	t.Setenv("ANTHROPIC_API_KEY", "from-env")

	key, err := LoadProviderAPIKey(prefs, "anthropic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "from-env" {
		t.Errorf("expected env key, got %q", key)
	}
}

func TestLoadProviderAPIKey_fromPrefs(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.OpenAIAPIKey = "sk-from-prefs"

	// Clear env var
	t.Setenv("OPENAI_API_KEY", "")

	key, err := LoadProviderAPIKey(prefs, "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk-from-prefs" {
		t.Errorf("expected prefs key, got %q", key)
	}
}

func TestLoadProviderAPIKey_mistralFromPrefs(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.MistralAPIKey = "mistral-from-prefs"

	t.Setenv("MISTRAL_API_KEY", "")

	key, err := LoadProviderAPIKey(prefs, "mistral")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "mistral-from-prefs" {
		t.Errorf("expected prefs key, got %q", key)
	}
}

func TestLoadProviderAPIKey_grokFromPrefs(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.GrokAPIKey = "grok-from-prefs"

	t.Setenv("XAI_API_KEY", "")

	key, err := LoadProviderAPIKey(prefs, "grok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "grok-from-prefs" {
		t.Errorf("expected prefs key, got %q", key)
	}
}

func TestLoadProviderAPIKey_zaiFromPrefs(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.ZAIAPIKey = "zai-from-prefs"

	t.Setenv("ZAI_API_KEY", "")

	key, err := LoadProviderAPIKey(prefs, "zai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "zai-from-prefs" {
		t.Errorf("expected prefs key, got %q", key)
	}
}

func TestLoadProviderAPIKey_ollamaNoKey(t *testing.T) {
	prefs := DefaultPreferences()

	key, err := LoadProviderAPIKey(prefs, "ollama")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "" {
		t.Errorf("expected empty key for ollama, got %q", key)
	}
}

func TestLoadProviderAPIKey_missingReturnsError(t *testing.T) {
	prefs := DefaultPreferences()
	t.Setenv("OPENAI_API_KEY", "")

	_, err := LoadProviderAPIKey(prefs, "openai")
	if err == nil {
		t.Fatal("expected error when no key available")
	}
}

func TestSavePreferences_filePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits not meaningful on Windows")
	}

	dir := t.TempDir()
	origConfigDir := configDirOverride
	configDirOverride = dir
	t.Cleanup(func() { configDirOverride = origConfigDir })

	p := DefaultPreferences()
	p.AnthropicAPIKey = "sk-test-key-1234"

	if err := SavePreferences(p); err != nil {
		t.Fatalf("SavePreferences: %v", err)
	}

	configPath := filepath.Join(dir, "config.json")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("config file permissions = %o, want 0600", perm)
	}
}

func TestLoadTelegramConfigFromPrefs(t *testing.T) {
	t.Run("from preferences", func(t *testing.T) {
		prefs := DefaultPreferences()
		prefs.TelegramBotToken = "123456:test-token"
		prefs.TelegramAllowedIDs = []int64{111, 222}

		cfg, err := LoadTelegramConfigFromPrefs(prefs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.BotToken != "123456:test-token" {
			t.Errorf("expected bot token from prefs, got %q", cfg.BotToken)
		}
		if len(cfg.AllowedIDs) != 2 {
			t.Errorf("expected 2 allowed IDs, got %d", len(cfg.AllowedIDs))
		}
	})

	t.Run("empty token returns error", func(t *testing.T) {
		prefs := DefaultPreferences()
		prefs.TelegramBotToken = ""

		_, err := LoadTelegramConfigFromPrefs(prefs)
		if err == nil {
			t.Fatal("expected error when no telegram bot token set")
		}
	})
}

func TestValidConfigKeys(t *testing.T) {
	keys := ValidConfigKeys()
	if len(keys) == 0 {
		t.Fatal("expected non-empty valid config keys")
	}

	// Every key from ConfigGroupDefs should be in the valid set
	for _, g := range ConfigGroupDefs {
		for _, k := range g.Keys {
			found := false
			for _, vk := range keys {
				if vk == k {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("config group key %q not in ValidConfigKeys()", k)
			}
		}
	}

	// Setting a valid key should succeed (not return "unknown key" error)
	p := DefaultPreferences()
	for _, k := range keys {
		err := p.Set(k, "test-value-123")
		if err != nil && strings.Contains(err.Error(), "unknown key") {
			t.Errorf("ValidConfigKeys() includes %q but Set() says unknown key", k)
		}
	}
}

func TestSanitizeValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean string", "sk-abc123", "sk-abc123"},
		{"null bytes", "\x00sk-abc\x00123", "sk-abc123"},
		{"leading null bytes", "\x00\x00RkR4abc", "RkR4abc"},
		{"mixed control chars", "\x01\x02hello\x03", "hello"},
		{"preserves newlines", "line1\nline2", "line1\nline2"},
		{"preserves tabs", "col1\tcol2", "col1\tcol2"},
		{"trims whitespace", "  sk-abc123  ", "sk-abc123"},
		{"null and whitespace", " \x00sk-abc\x00 ", "sk-abc"},
		{"empty string", "", ""},
		{"only null bytes", "\x00\x00\x00", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSet_sanitizesAPIKeys(t *testing.T) {
	tests := []struct {
		key   string
		input string
		want  string // raw stored value (not masked)
	}{
		{"anthropic.api_key", "\x00sk-ant-test1234", "sk-ant-test1234"},
		{"fireworks.api_key", "fw-\x00key\x00-5678", "fw-key-5678"},
		{"x.client_secret", "cs-\x01\x02value", "cs-value"},
		{"x.access_token", "\x00access-tok", "access-tok"},
		{"x.refresh_token", "refresh\x00tok", "refreshtok"},
		{"telegram.bot_token", "\x00123456:ABC", "123456:ABC"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			p := DefaultPreferences()
			if err := p.Set(tt.key, tt.input); err != nil {
				t.Fatalf("Set(%q, %q) error: %v", tt.key, tt.input, err)
			}
			// Read raw field value via a helper to verify sanitization
			var got string
			switch tt.key {
			case "anthropic.api_key":
				got = p.AnthropicAPIKey
			case "fireworks.api_key":
				got = p.FireworksAPIKey
			case "x.client_secret":
				got = p.XClientSecret
			case "x.access_token":
				got = p.XAccessToken
			case "x.refresh_token":
				got = p.XRefreshToken
			case "telegram.bot_token":
				got = p.TelegramBotToken
			}
			if got != tt.want {
				t.Errorf("after Set(%q, %q): raw value = %q, want %q", tt.key, tt.input, got, tt.want)
			}
		})
	}
}

func TestSet_doesNotSanitizeNonSensitiveKeys(t *testing.T) {
	p := DefaultPreferences()
	// model is not a sensitive key, value should be stored as-is (after no sanitization)
	if err := p.Set("model", "my-model"); err != nil {
		t.Fatalf("Set error: %v", err)
	}
	if p.Model != "my-model" {
		t.Errorf("model = %q, want %q", p.Model, "my-model")
	}
}

func TestSanitizePreferences(t *testing.T) {
	p := Preferences{
		AnthropicAPIKey: "\x00sk-ant-test",
		FireworksAPIKey: "fw-\x00key",
		XClientSecret:   "cs\x00val",
		TelegramBotToken: "\x00tok",
	}
	sanitizePreferences(&p)
	if p.AnthropicAPIKey != "sk-ant-test" {
		t.Errorf("AnthropicAPIKey = %q, want %q", p.AnthropicAPIKey, "sk-ant-test")
	}
	if p.FireworksAPIKey != "fw-key" {
		t.Errorf("FireworksAPIKey = %q, want %q", p.FireworksAPIKey, "fw-key")
	}
	if p.XClientSecret != "csval" {
		t.Errorf("XClientSecret = %q, want %q", p.XClientSecret, "csval")
	}
	if p.TelegramBotToken != "tok" {
		t.Errorf("TelegramBotToken = %q, want %q", p.TelegramBotToken, "tok")
	}
}

func TestSet_rejectsUnknownKey(t *testing.T) {
	p := DefaultPreferences()
	err := p.Set("malicious.key$(whoami)", "value")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("expected 'unknown key' error, got: %v", err)
	}
}
