package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/provider"
)

// smsHTTPClient is overridable in tests.
var smsHTTPClient = &http.Client{Timeout: 15 * time.Second}

// smsTextURL is the Textbelt endpoint for sending SMS.
var smsTextURL = "https://textbelt.com/text"

// smsStatusURL is the Textbelt endpoint for checking delivery status.
// Use fmt.Sprintf(smsStatusURL, textID).
var smsStatusURL = "https://textbelt.com/status/%s"

// ---------------------------------------------------------------------------
// sms_send
// ---------------------------------------------------------------------------

func smsSendTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "sms_send",
			Description: "Send an SMS message via the Textbelt API. Requires a Textbelt API key configured via /config set textbelt.api_key <key>.",
			Properties: map[string]provider.ToolProp{
				"phone":   {Type: "string", Description: "Phone number to send to. U.S./Canada: 10-digit with area code. International: E.164 format with country code (e.g. +44...)"},
				"message": {Type: "string", Description: "The SMS message content"},
			},
			Required: []string{"phone", "message"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			phone, ok := input["phone"].(string)
			if !ok || phone == "" {
				return "", fmt.Errorf("phone is required")
			}
			message, ok := input["message"].(string)
			if !ok || message == "" {
				return "", fmt.Errorf("message is required")
			}

			apiKey := ""
			if ctx != nil {
				apiKey = ctx.TextbeltAPIKey
			}
			if apiKey == "" {
				return "", fmt.Errorf("Textbelt API key not configured. Use /config set textbelt.api_key <key>")
			}

			return textbeltSend(phone, message, apiKey)
		},
	}
}

// textbeltSendResponse is the JSON response from the /text endpoint.
type textbeltSendResponse struct {
	Success        bool   `json:"success"`
	QuotaRemaining int    `json:"quotaRemaining"`
	TextID         int    `json:"textId"`
	Error          string `json:"error"`
}

// textbeltSend sends an SMS via the Textbelt API.
func textbeltSend(phone, message, apiKey string) (string, error) {
	form := url.Values{
		"phone":   {phone},
		"message": {message},
		"key":     {apiKey},
	}

	resp, err := smsHTTPClient.PostForm(smsTextURL, form)
	if err != nil {
		return "", fmt.Errorf("sending SMS: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var result textbeltSendResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return "", fmt.Errorf("SMS failed: %s", errMsg)
	}

	return fmt.Sprintf("SMS sent to %s (textId: %d, quota remaining: %d)", phone, result.TextID, result.QuotaRemaining), nil
}

// ---------------------------------------------------------------------------
// sms_status
// ---------------------------------------------------------------------------

func smsStatusTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "sms_status",
			Description: "Check the delivery status of a previously sent SMS using its text ID from sms_send.",
			Properties: map[string]provider.ToolProp{
				"text_id": {Type: "string", Description: "The text ID returned by sms_send"},
			},
			Required: []string{"text_id"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			textID, ok := input["text_id"].(string)
			if !ok || textID == "" {
				return "", fmt.Errorf("text_id is required")
			}

			return textbeltStatus(textID)
		},
	}
}

// textbeltStatusResponse is the JSON response from the /status endpoint.
type textbeltStatusResponse struct {
	Status string `json:"status"` // "DELIVERED", "SENT", "SENDING", "FAILED", "UNKNOWN"
}

// textbeltStatus checks SMS delivery status.
func textbeltStatus(textID string) (string, error) {
	statusURL := fmt.Sprintf(smsStatusURL, textID)

	resp, err := smsHTTPClient.Get(statusURL)
	if err != nil {
		return "", fmt.Errorf("checking SMS status: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var result textbeltStatusResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return fmt.Sprintf("SMS %s: %s", textID, result.Status), nil
}

// ---------------------------------------------------------------------------
// sms_schedule
// ---------------------------------------------------------------------------

func smsScheduleTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "sms_schedule",
			Description: "Schedule an SMS for later sending. Requires scheduler support and a valid time.",
			Properties: map[string]provider.ToolProp{
				"phone":      {Type: "string", Description: "Phone number to send to"},
				"message":    {Type: "string", Description: "The SMS message content"},
				"time":       {Type: "string", Description: "Schedule time: RFC3339 or HH:MM (local time)"},
				"recurrence": {Type: "string", Description: "Optional recurrence: once, daily, hourly"},
			},
			Required: []string{"phone", "message", "time"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.ScheduleTool == nil {
				return "", fmt.Errorf("SMS scheduler is not available in this runtime")
			}

			phone, ok := input["phone"].(string)
			if !ok || phone == "" {
				return "", fmt.Errorf("phone is required")
			}
			message, ok := input["message"].(string)
			if !ok || message == "" {
				return "", fmt.Errorf("message is required")
			}
			timeStr, ok := input["time"].(string)
			if !ok || timeStr == "" {
				return "", fmt.Errorf("time is required")
			}

			apiKey := ""
			if ctx != nil {
				apiKey = ctx.TextbeltAPIKey
			}
			if apiKey == "" {
				return "", fmt.Errorf("Textbelt API key not configured. Use /config set textbelt.api_key <key>")
			}

			scheduledFor, err := parseScheduleTime(timeStr)
			if err != nil {
				return "", err
			}

			recurrence := "once"
			if v, ok := input["recurrence"].(string); ok && v != "" {
				recurrence = strings.ToLower(v)
			}

			toolInput := map[string]any{
				"phone":   phone,
				"message": message,
			}

			id, err := ctx.ScheduleTool("sms_send", toolInput, scheduledFor, recurrence)
			if err != nil {
				return "", fmt.Errorf("scheduling SMS: %w", err)
			}

			return fmt.Sprintf("Scheduled SMS to %s for %s (id: %s, recurrence: %s)",
				phone, scheduledFor.Format(time.RFC3339), id, recurrence), nil
		},
	}
}

// parseScheduleTime parses a time string as RFC3339 or HH:MM local time.
func parseScheduleTime(s string) (time.Time, error) {
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try HH:MM as local time today/tomorrow.
	if len(s) == 5 && s[2] == ':' {
		now := nowFunc()
		t, err := time.ParseInLocation("15:04", s, now.Location())
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time format %q: use RFC3339 or HH:MM", s)
		}
		t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if t.Before(now) {
			t = t.Add(24 * time.Hour)
		}
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time format %q: use RFC3339 or HH:MM", s)
}
