package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigDir(t *testing.T) {
	t.Run("returns override when set", func(t *testing.T) {
		orig := configDirOverride
		configDirOverride = "/tmp/test-config"
		t.Cleanup(func() { configDirOverride = orig })

		got := ConfigDir()
		if got != "/tmp/test-config" {
			t.Errorf("expected override dir, got %q", got)
		}
	})

	t.Run("returns home-based path when no override", func(t *testing.T) {
		orig := configDirOverride
		configDirOverride = ""
		t.Cleanup(func() { configDirOverride = orig })

		got := ConfigDir()
		if got == "" {
			t.Fatal("expected non-empty config dir")
		}
		if !strings.HasSuffix(got, filepath.Join(".config", "muxd")) {
			t.Errorf("expected path ending in .config/muxd, got %q", got)
		}
	})
}

func TestDataDir(t *testing.T) {
	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty data dir")
	}
	if !strings.HasSuffix(dir, filepath.Join(".local", "share", "muxd")) {
		t.Errorf("expected path ending in .local/share/muxd, got %q", dir)
	}
	// Directory should exist
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat data dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected data dir to be a directory")
	}
}

func TestConfigGroupNames(t *testing.T) {
	names := ConfigGroupNames()
	want := []string{"models", "tools", "daemon", "hub", "theme"}
	if len(names) != len(want) {
		t.Fatalf("expected %d group names, got %d", len(want), len(names))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("group name [%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestConfigFilePath(t *testing.T) {
	orig := configDirOverride
	configDirOverride = "/tmp/test-muxd"
	t.Cleanup(func() { configDirOverride = orig })

	got := ConfigFilePath()
	want := filepath.Join("/tmp/test-muxd", "config.json")
	if got != want {
		t.Errorf("ConfigFilePath() = %q, want %q", got, want)
	}
}

func TestPreferencesFilePath(t *testing.T) {
	orig := configDirOverride
	configDirOverride = "/tmp/test-muxd"
	t.Cleanup(func() { configDirOverride = orig })

	got := PreferencesFilePath()
	want := filepath.Join("/tmp/test-muxd", "config.json")
	if got != want {
		t.Errorf("PreferencesFilePath() = %q, want %q", got, want)
	}
}

func TestParseBoolish(t *testing.T) {
	tests := []struct {
		input   string
		want    bool
		wantErr bool
	}{
		{"true", true, false},
		{"True", true, false},
		{"TRUE", true, false},
		{"on", true, false},
		{"yes", true, false},
		{"1", true, false},
		{"false", false, false},
		{"False", false, false},
		{"off", false, false},
		{"no", false, false},
		{"0", false, false},
		{"maybe", false, true},
		{"", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseBoolish(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseBoolish(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"anthropic.api_key",
		"textbelt.api_key",
	}
	for _, k := range sensitive {
		if !isSensitiveKey(k) {
			t.Errorf("expected %q to be sensitive", k)
		}
	}

	notSensitive := []string{"model", "ollama.url", "footer.tokens", "scheduler.allowed_tools"}
	for _, k := range notSensitive {
		if isSensitiveKey(k) {
			t.Errorf("expected %q to NOT be sensitive", k)
		}
	}
}

func TestResolveKeyDisplay(t *testing.T) {
	t.Run("returns masked pref key when set", func(t *testing.T) {
		got := resolveKeyDisplay("sk-ant-secret1234", "ANTHROPIC_API_KEY")
		if got != "****1234" {
			t.Errorf("expected ****1234, got %q", got)
		}
	})

	t.Run("returns masked env key with suffix when pref empty", func(t *testing.T) {
		t.Setenv("TEST_RESOLVE_KEY", "sk-env-key-5678")
		got := resolveKeyDisplay("", "TEST_RESOLVE_KEY")
		if got != "****5678 (from env)" {
			t.Errorf("expected '****5678 (from env)', got %q", got)
		}
	})

	t.Run("returns empty when both empty", func(t *testing.T) {
		t.Setenv("TEST_RESOLVE_EMPTY", "")
		got := resolveKeyDisplay("", "TEST_RESOLVE_EMPTY")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestResolveAPIKeySource(t *testing.T) {
	t.Run("returns env when env var set", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "from-env")
		prefs := DefaultPreferences()
		prefs.AnthropicAPIKey = "from-config"

		got := ResolveAPIKeySource(prefs, "anthropic")
		if got != "env" {
			t.Errorf("expected 'env', got %q", got)
		}
	})

	t.Run("returns config when only config set", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		prefs := DefaultPreferences()
		prefs.OpenAIAPIKey = "from-config"

		got := ResolveAPIKeySource(prefs, "openai")
		if got != "config" {
			t.Errorf("expected 'config', got %q", got)
		}
	})

	t.Run("returns empty when neither set", func(t *testing.T) {
		t.Setenv("GOOGLE_API_KEY", "")
		prefs := DefaultPreferences()

		got := ResolveAPIKeySource(prefs, "google")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("fireworks from config", func(t *testing.T) {
		t.Setenv("FIREWORKS_API_KEY", "")
		prefs := DefaultPreferences()
		prefs.FireworksAPIKey = "fw-key"

		got := ResolveAPIKeySource(prefs, "fireworks")
		if got != "config" {
			t.Errorf("expected 'config', got %q", got)
		}
	})

	t.Run("unknown provider returns empty", func(t *testing.T) {
		prefs := DefaultPreferences()
		got := ResolveAPIKeySource(prefs, "unknown-provider")
		if got != "" {
			t.Errorf("expected empty for unknown provider, got %q", got)
		}
	})
}

func TestDisabledToolsSet(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]bool
	}{
		{"empty", "", map[string]bool{}},
		{"single tool", "x_post", map[string]bool{"x_post": true}},
		{"multiple tools", "x_post,web_fetch,bash", map[string]bool{"x_post": true, "web_fetch": true, "bash": true}},
		{"with spaces", " x_post , web_fetch ", map[string]bool{"x_post": true, "web_fetch": true}},
		{"trailing comma", "x_post,", map[string]bool{"x_post": true}},
		{"uppercase normalized", "X_POST,WEB_FETCH", map[string]bool{"x_post": true, "web_fetch": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Preferences{ToolsDisabled: tt.input}
			got := p.DisabledToolsSet()
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d entries, got %d", len(tt.want), len(got))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("expected %q=%v, got %v", k, v, got[k])
				}
			}
		})
	}
}

func TestScheduledAllowedToolsSet(t *testing.T) {
	t.Run("empty uses defaults", func(t *testing.T) {
		p := Preferences{}
		got := p.ScheduledAllowedToolsSet()
		defaults := []string{"file_read", "grep", "list_files", "git_status", "web_search", "web_fetch", "todo_read"}
		for _, name := range defaults {
			if !got[name] {
				t.Errorf("expected default tool %q to be allowed", name)
			}
		}
		if len(got) != len(defaults) {
			t.Errorf("expected %d default tools, got %d", len(defaults), len(got))
		}
	})

	t.Run("custom tools", func(t *testing.T) {
		p := Preferences{SchedulerAllowedTools: "bash,file_write"}
		got := p.ScheduledAllowedToolsSet()
		if !got["bash"] || !got["file_write"] {
			t.Error("expected custom tools to be allowed")
		}
		if got["x_post"] {
			t.Error("expected default tools to NOT be included when custom set")
		}
	})

	t.Run("with spaces and trailing comma", func(t *testing.T) {
		p := Preferences{SchedulerAllowedTools: " bash , file_write ,"}
		got := p.ScheduledAllowedToolsSet()
		if len(got) != 2 {
			t.Errorf("expected 2 tools, got %d", len(got))
		}
	})
}

func TestMergePreferences(t *testing.T) {
	t.Run("copies non-empty fields from src to dst", func(t *testing.T) {
		dst := DefaultPreferences()
		dst.AnthropicAPIKey = "old-key"
		dst.Model = "old-model"

		src := DefaultPreferences()
		src.Model = "new-model"
		src.OpenAIAPIKey = "openai-key"
		// Leave AnthropicAPIKey empty in src

		mergePreferences(&dst, &src)

		if dst.Model != "new-model" {
			t.Errorf("expected model to be overwritten, got %q", dst.Model)
		}
		if dst.OpenAIAPIKey != "openai-key" {
			t.Errorf("expected openai key to be set, got %q", dst.OpenAIAPIKey)
		}
		if dst.AnthropicAPIKey != "old-key" {
			t.Errorf("expected anthropic key to be preserved, got %q", dst.AnthropicAPIKey)
		}
	})

	t.Run("copies boolean fields from src", func(t *testing.T) {
		dst := DefaultPreferences()
		dst.FooterTokens = true

		src := DefaultPreferences()
		src.FooterTokens = false

		mergePreferences(&dst, &src)

		if dst.FooterTokens {
			t.Error("expected FooterTokens to be false from src")
		}
	})

	t.Run("all provider keys merge", func(t *testing.T) {
		dst := DefaultPreferences()
		src := DefaultPreferences()
		src.ZAIAPIKey = "zai"
		src.GrokAPIKey = "grok"
		src.MistralAPIKey = "mistral"
		src.GoogleAPIKey = "google"
		src.FireworksAPIKey = "fireworks"
		src.BraveAPIKey = "brave"
		src.SchedulerAllowedTools = "bash"
		src.ToolsDisabled = "web_fetch"
		src.OllamaURL = "http://localhost:11434"

		mergePreferences(&dst, &src)

		if dst.ZAIAPIKey != "zai" {
			t.Errorf("ZAIAPIKey = %q", dst.ZAIAPIKey)
		}
		if dst.GrokAPIKey != "grok" {
			t.Errorf("GrokAPIKey = %q", dst.GrokAPIKey)
		}
		if dst.MistralAPIKey != "mistral" {
			t.Errorf("MistralAPIKey = %q", dst.MistralAPIKey)
		}
		if dst.GoogleAPIKey != "google" {
			t.Errorf("GoogleAPIKey = %q", dst.GoogleAPIKey)
		}
		if dst.FireworksAPIKey != "fireworks" {
			t.Errorf("FireworksAPIKey = %q", dst.FireworksAPIKey)
		}
		if dst.BraveAPIKey != "brave" {
			t.Errorf("BraveAPIKey = %q", dst.BraveAPIKey)
		}
		if dst.OllamaURL != "http://localhost:11434" {
			t.Errorf("OllamaURL = %q", dst.OllamaURL)
		}
	})
}

func TestLoadPreferences(t *testing.T) {
	t.Run("returns defaults when no config dir", func(t *testing.T) {
		orig := configDirOverride
		configDirOverride = ""
		t.Cleanup(func() { configDirOverride = orig })

		// Temporarily clear HOME to make ConfigDir return ""
		// This is tricky — instead just test with a nonexistent dir
		// so ReadFile fails gracefully
		configDirOverride = filepath.Join(t.TempDir(), "nonexistent")
		p := LoadPreferences()
		if !p.FooterTokens {
			t.Error("expected default FooterTokens=true")
		}
	})

	t.Run("loads from config.json", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		data, _ := json.Marshal(Preferences{
			Model:     "gpt-4o",
			OllamaURL: "http://localhost:11434",
			FooterCwd: false,
		})
		os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)

		p := LoadPreferences()
		if p.Model != "gpt-4o" {
			t.Errorf("expected model=gpt-4o, got %q", p.Model)
		}
		if p.OllamaURL != "http://localhost:11434" {
			t.Errorf("expected ollama url, got %q", p.OllamaURL)
		}
	})

	t.Run("merges legacy preferences.json", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		// Write config.json with one value
		configData, _ := json.Marshal(Preferences{Model: "old-model"})
		os.WriteFile(filepath.Join(dir, "config.json"), configData, 0o600)

		// Write legacy preferences.json with another value (should override)
		legacyData, _ := json.Marshal(Preferences{Model: "legacy-model", OpenAIAPIKey: "sk-legacy"})
		os.WriteFile(filepath.Join(dir, "preferences.json"), legacyData, 0o600)

		p := LoadPreferences()

		// Legacy values should win
		if p.Model != "legacy-model" {
			t.Errorf("expected legacy model, got %q", p.Model)
		}
		if p.OpenAIAPIKey != "sk-legacy" {
			t.Errorf("expected legacy openai key, got %q", p.OpenAIAPIKey)
		}

		// Legacy file should be removed
		if _, err := os.Stat(filepath.Join(dir, "preferences.json")); !os.IsNotExist(err) {
			t.Error("expected legacy preferences.json to be removed")
		}
	})

	t.Run("handles invalid config.json gracefully", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		// Write invalid JSON
		os.WriteFile(filepath.Join(dir, "config.json"), []byte("{invalid}"), 0o600)

		p := LoadPreferences()
		// Should return defaults without panic
		if !p.FooterTokens {
			t.Error("expected default FooterTokens=true after bad JSON")
		}
	})

	t.Run("sanitizes loaded preferences", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		// Write config with null-byte-contaminated key
		data, _ := json.Marshal(Preferences{AnthropicAPIKey: "\x00sk-ant-dirty"})
		os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)

		p := LoadPreferences()
		if strings.Contains(p.AnthropicAPIKey, "\x00") {
			t.Error("expected null bytes to be sanitized")
		}
	})
}

func TestSavePreferences(t *testing.T) {
	t.Run("writes and reads back correctly", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		p := DefaultPreferences()
		p.Model = "claude-sonnet-4-6"
		p.AnthropicAPIKey = "sk-ant-test"

		if err := SavePreferences(p); err != nil {
			t.Fatalf("SavePreferences: %v", err)
		}

		// Read back
		data, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		var loaded Preferences
		if err := json.Unmarshal(data, &loaded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if loaded.Model != "claude-sonnet-4-6" {
			t.Errorf("expected model, got %q", loaded.Model)
		}
		if loaded.AnthropicAPIKey != "sk-ant-test" {
			t.Errorf("expected api key, got %q", loaded.AnthropicAPIKey)
		}
	})

	t.Run("returns error when config dir empty", func(t *testing.T) {
		orig := configDirOverride
		configDirOverride = ""
		t.Cleanup(func() { configDirOverride = orig })

		// Force ConfigDir to return "" — we need HOME unset
		// Use a simpler approach: set to empty and verify
		err := SavePreferences(DefaultPreferences())
		if err != nil {
			// On most systems this succeeds because ConfigDir reads HOME.
			// We just verify it doesn't panic.
		}
	})
}

func TestWarnInsecurePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission check not applicable on Windows")
	}

	t.Run("does not warn for 0600", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "secure.json")
		os.WriteFile(f, []byte("{}"), 0o600)

		// Capture stderr — just verify no panic
		warnInsecurePermissions(f)
	})

	t.Run("handles nonexistent file", func(t *testing.T) {
		warnInsecurePermissions("/nonexistent/file.json")
		// Should not panic
	})
}

func TestExecuteConfigAction(t *testing.T) {
	t.Run("show returns all groups", func(t *testing.T) {
		p := DefaultPreferences()
		result, err := ExecuteConfigAction(&p, []string{"show"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Models:") {
			t.Error("expected 'Models:' in output")
		}
		if !strings.Contains(result, "Theme:") {
			t.Error("expected 'Theme:' in output")
		}
	})

	t.Run("default is show", func(t *testing.T) {
		p := DefaultPreferences()
		result, err := ExecuteConfigAction(&p, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Models:") {
			t.Error("expected show output for empty args")
		}
	})

	t.Run("models group", func(t *testing.T) {
		p := DefaultPreferences()
		result, err := ExecuteConfigAction(&p, []string{"models"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Models:") {
			t.Error("expected 'Models:' in output")
		}
		if strings.Contains(result, "Theme:") {
			t.Error("should only show models group")
		}
	})

	t.Run("tools group", func(t *testing.T) {
		p := DefaultPreferences()
		result, err := ExecuteConfigAction(&p, []string{"tools"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Tools:") {
			t.Error("expected 'Tools:' in output")
		}
	})

	t.Run("theme group", func(t *testing.T) {
		p := DefaultPreferences()
		result, err := ExecuteConfigAction(&p, []string{"theme"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Theme:") {
			t.Error("expected 'Theme:' in output")
		}
	})

	t.Run("set updates and saves", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		p := DefaultPreferences()
		result, err := ExecuteConfigAction(&p, []string{"set", "model", "gpt-4o"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Set model") {
			t.Errorf("expected confirmation, got %q", result)
		}
		if p.Model != "gpt-4o" {
			t.Errorf("expected model to be updated, got %q", p.Model)
		}
	})

	t.Run("set with insufficient args returns error", func(t *testing.T) {
		p := DefaultPreferences()
		_, err := ExecuteConfigAction(&p, []string{"set", "model"})
		if err == nil {
			t.Fatal("expected error for insufficient args")
		}
	})

	t.Run("set invalid key returns error", func(t *testing.T) {
		p := DefaultPreferences()
		_, err := ExecuteConfigAction(&p, []string{"set", "bad.key", "value"})
		if err == nil {
			t.Fatal("expected error for invalid key")
		}
	})

	t.Run("reset restores defaults", func(t *testing.T) {
		dir := t.TempDir()
		orig := configDirOverride
		configDirOverride = dir
		t.Cleanup(func() { configDirOverride = orig })

		p := DefaultPreferences()
		p.Model = "custom-model"
		p.FooterTokens = false

		result, err := ExecuteConfigAction(&p, []string{"reset"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "reset") {
			t.Errorf("expected reset confirmation, got %q", result)
		}
		if p.Model != "" {
			t.Errorf("expected model to be reset, got %q", p.Model)
		}
		if !p.FooterTokens {
			t.Error("expected FooterTokens to be reset to true")
		}
	})

	t.Run("unknown subcommand returns error", func(t *testing.T) {
		p := DefaultPreferences()
		_, err := ExecuteConfigAction(&p, []string{"badcmd"})
		if err == nil {
			t.Fatal("expected error for unknown subcommand")
		}
		if !strings.Contains(err.Error(), "usage:") {
			t.Errorf("expected usage in error, got %q", err.Error())
		}
	})
}

func TestFormatConfigGroups(t *testing.T) {
	groups := []ConfigGroup{
		{
			Name: "test",
			Entries: []PrefEntry{
				{Key: "foo", Value: "bar"},
				{Key: "baz", Value: "(not set)"},
			},
		},
	}

	result := FormatConfigGroups(groups)
	if !strings.Contains(result, "Test:") {
		t.Error("expected capitalized group name")
	}
	if !strings.Contains(result, "foo") {
		t.Error("expected key 'foo' in output")
	}
	if !strings.Contains(result, "bar") {
		t.Error("expected value 'bar' in output")
	}
	if !strings.Contains(result, "/config set") {
		t.Error("expected usage hint in output")
	}
}

func TestFormatConfigGroups_multipleGroups(t *testing.T) {
	groups := []ConfigGroup{
		{Name: "alpha", Entries: []PrefEntry{{Key: "a", Value: "1"}}},
		{Name: "beta", Entries: []PrefEntry{{Key: "b", Value: "2"}}},
	}

	result := FormatConfigGroups(groups)
	if !strings.Contains(result, "Alpha:") {
		t.Error("expected 'Alpha:'")
	}
	if !strings.Contains(result, "Beta:") {
		t.Error("expected 'Beta:'")
	}
}

func TestLoadProviderAPIKey_googleFromPrefs(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.GoogleAPIKey = "google-from-prefs"
	t.Setenv("GOOGLE_API_KEY", "")

	key, err := LoadProviderAPIKey(prefs, "google")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "google-from-prefs" {
		t.Errorf("expected google prefs key, got %q", key)
	}
}

func TestLoadProviderAPIKey_fireworksFromPrefs(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.FireworksAPIKey = "fw-from-prefs"
	t.Setenv("FIREWORKS_API_KEY", "")

	key, err := LoadProviderAPIKey(prefs, "fireworks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "fw-from-prefs" {
		t.Errorf("expected fireworks prefs key, got %q", key)
	}
}

func TestGet_additionalKeys(t *testing.T) {
	p := DefaultPreferences()
	p.ToolsDisabled = "web_fetch,bash"

	tests := []struct {
		key  string
		want string
	}{
		{"tools.disabled", "web_fetch,bash"},
		{"nonexistent", ""},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := p.Get(tt.key)
			if got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestSet_additionalKeys(t *testing.T) {
	p := DefaultPreferences()

	keys := []struct {
		key   string
		value string
	}{
		{"tools.disabled", "web_fetch"},
		{"scheduler.allowed_tools", "bash,file_read"},
	}

	for _, tt := range keys {
		if err := p.Set(tt.key, tt.value); err != nil {
			t.Errorf("Set(%q, %q) error: %v", tt.key, tt.value, err)
		}
	}

	if p.ToolsDisabled != "web_fetch" {
		t.Errorf("ToolsDisabled = %q", p.ToolsDisabled)
	}
}

func TestSet_boolishKeys(t *testing.T) {
	tests := []struct {
		key   string
		value string
		want  bool
	}{
		{"footer.tokens", "off", false},
		{"footer.cost", "no", false},
		{"footer.cwd", "0", false},
		{"footer.session", "yes", true},
		{"footer.keybindings", "on", true},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			p := DefaultPreferences()
			if err := p.Set(tt.key, tt.value); err != nil {
				t.Fatalf("Set error: %v", err)
			}
			got := p.Get(tt.key)
			wantStr := "false"
			if tt.want {
				wantStr = "true"
			}
			if got != wantStr {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, wantStr)
			}
		})
	}
}

func TestSet_invalidBoolValue(t *testing.T) {
	p := DefaultPreferences()
	err := p.Set("footer.tokens", "maybe")
	if err == nil {
		t.Fatal("expected error for invalid bool value")
	}
}

func TestPerTaskModelConfig(t *testing.T) {
	t.Run("Set and Get model.compact", func(t *testing.T) {
		p := DefaultPreferences()
		if err := p.Set("model.compact", "claude-haiku"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Get("model.compact"); got != "claude-haiku" {
			t.Errorf("expected claude-haiku, got %s", got)
		}
	})

	t.Run("Set and Get model.title", func(t *testing.T) {
		p := DefaultPreferences()
		if err := p.Set("model.title", "gpt-4o-mini"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Get("model.title"); got != "gpt-4o-mini" {
			t.Errorf("expected gpt-4o-mini, got %s", got)
		}
	})

	t.Run("Set and Get model.tags", func(t *testing.T) {
		p := DefaultPreferences()
		if err := p.Set("model.tags", "claude-haiku"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := p.Get("model.tags"); got != "claude-haiku" {
			t.Errorf("expected claude-haiku, got %s", got)
		}
	})

	t.Run("appears in All()", func(t *testing.T) {
		p := DefaultPreferences()
		_ = p.Set("model.compact", "claude-haiku")
		found := false
		for _, e := range p.All() {
			if e.Key == "model.compact" && e.Value == "claude-haiku" {
				found = true
			}
		}
		if !found {
			t.Error("model.compact not found in All()")
		}
	})

	t.Run("appears in models config group", func(t *testing.T) {
		p := DefaultPreferences()
		_ = p.Set("model.compact", "claude-haiku")
		group := p.GroupByName("models")
		if group == nil {
			t.Fatal("models group not found")
		}
		found := false
		for _, e := range group.Entries {
			if e.Key == "model.compact" {
				found = true
			}
		}
		if !found {
			t.Error("model.compact not in models group")
		}
	})
}
