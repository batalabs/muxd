package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProviderEnvVars maps provider names to their environment variable names.
var ProviderEnvVars = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"zai":       "ZAI_API_KEY",
	"grok":      "XAI_API_KEY",
	"mistral":   "MISTRAL_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"google":    "GOOGLE_API_KEY",
	"fireworks": "FIREWORKS_API_KEY",
}

// KnownProviders lists valid provider names for validation.
var KnownProviders = []string{"anthropic", "zai", "grok", "mistral", "openai", "google", "ollama", "fireworks"}

// configDirOverride is set by tests to redirect ConfigDir.
var configDirOverride string

// ConfigDir returns the config directory for muxd.
func ConfigDir() string {
	if configDirOverride != "" {
		return configDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "muxd")
}

// DataDir returns ~/.local/share/muxd, creating it if needed.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "muxd")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// LoadProviderAPIKey resolves an API key for the given provider using:
//  1. Environment variable (e.g. ANTHROPIC_API_KEY, OPENAI_API_KEY)
//  2. Preferences (e.g. anthropic_api_key set via /config)
//
// Ollama returns empty string (no key needed).
func LoadProviderAPIKey(prefs Preferences, providerName string) (string, error) {
	if providerName == "ollama" {
		return "", nil
	}

	// 1. Check environment variable
	if envVar, ok := ProviderEnvVars[providerName]; ok {
		if key := strings.TrimSpace(os.Getenv(envVar)); key != "" {
			return key, nil
		}
	}

	// 2. Check preferences
	switch providerName {
	case "anthropic":
		if key := strings.TrimSpace(prefs.AnthropicAPIKey); key != "" {
			return key, nil
		}
	case "openai":
		if key := strings.TrimSpace(prefs.OpenAIAPIKey); key != "" {
			return key, nil
		}
	case "mistral":
		if key := strings.TrimSpace(prefs.MistralAPIKey); key != "" {
			return key, nil
		}
	case "grok":
		if key := strings.TrimSpace(prefs.GrokAPIKey); key != "" {
			return key, nil
		}
	case "zai":
		if key := strings.TrimSpace(prefs.ZAIAPIKey); key != "" {
			return key, nil
		}
	case "google":
		if key := strings.TrimSpace(prefs.GoogleAPIKey); key != "" {
			return key, nil
		}
	case "fireworks":
		if key := strings.TrimSpace(prefs.FireworksAPIKey); key != "" {
			return key, nil
		}
	}

	return "", fmt.Errorf("no API key found for %s: set %s or use /config set %s.api_key <key>",
		providerName, ProviderEnvVars[providerName], providerName)
}

// ResolveAPIKeySource returns the source of the API key for display purposes.
// Returns "env", "config", or "" if not found.
func ResolveAPIKeySource(prefs Preferences, providerName string) string {
	if envVar, ok := ProviderEnvVars[providerName]; ok {
		if key := strings.TrimSpace(os.Getenv(envVar)); key != "" {
			return "env"
		}
	}
	switch providerName {
	case "anthropic":
		if prefs.AnthropicAPIKey != "" {
			return "config"
		}
	case "openai":
		if prefs.OpenAIAPIKey != "" {
			return "config"
		}
	case "mistral":
		if prefs.MistralAPIKey != "" {
			return "config"
		}
	case "grok":
		if prefs.GrokAPIKey != "" {
			return "config"
		}
	case "zai":
		if prefs.ZAIAPIKey != "" {
			return "config"
		}
	case "google":
		if prefs.GoogleAPIKey != "" {
			return "config"
		}
	case "fireworks":
		if prefs.FireworksAPIKey != "" {
			return "config"
		}
	}
	return ""
}
