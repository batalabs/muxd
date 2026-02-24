package config

import "fmt"

// TelegramConfig holds Telegram bot configuration.
type TelegramConfig struct {
	BotToken   string  `json:"bot_token"`
	AllowedIDs []int64 `json:"allowed_user_ids"`
}

// LoadTelegramConfigFromPrefs builds a TelegramConfig from Preferences.
func LoadTelegramConfigFromPrefs(prefs Preferences) (TelegramConfig, error) {
	if prefs.TelegramBotToken == "" {
		return TelegramConfig{}, fmt.Errorf("telegram bot token not set: use /config set telegram.bot_token <token>")
	}
	return TelegramConfig{
		BotToken:   prefs.TelegramBotToken,
		AllowedIDs: prefs.TelegramAllowedIDs,
	}, nil
}
