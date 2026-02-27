package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// sms_send
// ---------------------------------------------------------------------------

func TestSmsSendTool(t *testing.T) {
	tool := smsSendTool()

	t.Run("missing API key returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"phone":   "5555555555",
			"message": "hello",
		}, &ToolContext{})
		if err == nil {
			t.Fatal("expected error for missing API key")
		}
		if !strings.Contains(err.Error(), "Textbelt API key") {
			t.Errorf("expected API key error, got: %v", err)
		}
	})

	t.Run("missing phone returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"message": "hello",
		}, &ToolContext{TextbeltAPIKey: "test"})
		if err == nil {
			t.Fatal("expected error for missing phone")
		}
	})

	t.Run("missing message returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"phone": "5555555555",
		}, &ToolContext{TextbeltAPIKey: "test"})
		if err == nil {
			t.Fatal("expected error for missing message")
		}
	})

	t.Run("successful send returns confirmation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parsing form: %v", err)
			}
			if r.FormValue("phone") != "5555555555" {
				t.Errorf("expected phone 5555555555, got %s", r.FormValue("phone"))
			}
			if r.FormValue("message") != "Hello world" {
				t.Errorf("expected message 'Hello world', got %s", r.FormValue("message"))
			}
			if r.FormValue("key") != "test-key" {
				t.Errorf("expected key test-key, got %s", r.FormValue("key"))
			}
			json.NewEncoder(w).Encode(textbeltSendResponse{
				Success:        true,
				QuotaRemaining: 40,
				TextID:         12345,
			})
		}))
		defer server.Close()

		origURL := smsTextURL
		origClient := smsHTTPClient
		smsTextURL = server.URL
		smsHTTPClient = server.Client()
		defer func() {
			smsTextURL = origURL
			smsHTTPClient = origClient
		}()

		result, err := tool.Execute(map[string]any{
			"phone":   "5555555555",
			"message": "Hello world",
		}, &ToolContext{TextbeltAPIKey: "test-key"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "SMS sent") {
			t.Errorf("expected success message, got: %s", result)
		}
		if !strings.Contains(result, "12345") {
			t.Errorf("expected textId in result, got: %s", result)
		}
		if !strings.Contains(result, "40") {
			t.Errorf("expected quota in result, got: %s", result)
		}
	})

	t.Run("API error is propagated", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(textbeltSendResponse{
				Success: false,
				Error:   "Out of quota",
			})
		}))
		defer server.Close()

		origURL := smsTextURL
		origClient := smsHTTPClient
		smsTextURL = server.URL
		smsHTTPClient = server.Client()
		defer func() {
			smsTextURL = origURL
			smsHTTPClient = origClient
		}()

		_, err := tool.Execute(map[string]any{
			"phone":   "5555555555",
			"message": "Hello",
		}, &ToolContext{TextbeltAPIKey: "test-key"})
		if err == nil {
			t.Fatal("expected error for failed SMS")
		}
		if !strings.Contains(err.Error(), "Out of quota") {
			t.Errorf("expected quota error, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// sms_status
// ---------------------------------------------------------------------------

func TestSmsStatusTool(t *testing.T) {
	tool := smsStatusTool()

	t.Run("missing text_id returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{}, nil)
		if err == nil {
			t.Fatal("expected error for missing text_id")
		}
	})

	t.Run("returns delivery status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			// Path should contain the text ID
			if !strings.Contains(r.URL.Path, "12345") {
				t.Errorf("expected path to contain text ID, got: %s", r.URL.Path)
			}
			json.NewEncoder(w).Encode(textbeltStatusResponse{
				Status: "DELIVERED",
			})
		}))
		defer server.Close()

		origURL := smsStatusURL
		origClient := smsHTTPClient
		smsStatusURL = server.URL + "/%s"
		smsHTTPClient = server.Client()
		defer func() {
			smsStatusURL = origURL
			smsHTTPClient = origClient
		}()

		result, err := tool.Execute(map[string]any{
			"text_id": "12345",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "DELIVERED") {
			t.Errorf("expected DELIVERED status, got: %s", result)
		}
	})
}

// ---------------------------------------------------------------------------
// sms_schedule
// ---------------------------------------------------------------------------

func TestSmsScheduleTool(t *testing.T) {
	tool := smsScheduleTool()

	t.Run("no scheduler returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"phone":   "5555555555",
			"message": "hello",
			"time":    "14:00",
		}, &ToolContext{TextbeltAPIKey: "test"})
		if err == nil {
			t.Fatal("expected error for missing scheduler")
		}
		if !strings.Contains(err.Error(), "not available") {
			t.Errorf("expected scheduler error, got: %v", err)
		}
	})

	t.Run("missing API key returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"phone":   "5555555555",
			"message": "hello",
			"time":    "14:00",
		}, &ToolContext{
			ScheduleTool: func(toolName string, input map[string]any, scheduledFor time.Time, recurrence string) (string, error) {
				return "job-1", nil
			},
		})
		if err == nil {
			t.Fatal("expected error for missing API key")
		}
	})

	t.Run("schedules SMS with RFC3339 time", func(t *testing.T) {
		var capturedTool string
		var capturedInput map[string]any
		var capturedRecurrence string

		ctx := &ToolContext{
			TextbeltAPIKey: "test-key",
			ScheduleTool: func(toolName string, input map[string]any, scheduledFor time.Time, recurrence string) (string, error) {
				capturedTool = toolName
				capturedInput = input
				capturedRecurrence = recurrence
				return "job-123", nil
			},
		}

		result, err := tool.Execute(map[string]any{
			"phone":      "5555555555",
			"message":    "Reminder",
			"time":       "2026-03-01T14:00:00Z",
			"recurrence": "daily",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedTool != "sms_send" {
			t.Errorf("expected tool sms_send, got: %s", capturedTool)
		}
		if capturedInput["phone"] != "5555555555" {
			t.Errorf("expected phone in input, got: %v", capturedInput)
		}
		if capturedInput["message"] != "Reminder" {
			t.Errorf("expected message in input, got: %v", capturedInput)
		}
		if capturedRecurrence != "daily" {
			t.Errorf("expected daily recurrence, got: %s", capturedRecurrence)
		}
		if !strings.Contains(result, "job-123") {
			t.Errorf("expected job ID in result, got: %s", result)
		}
	})

	t.Run("schedules SMS with HH:MM time", func(t *testing.T) {
		origNow := nowFunc
		nowFunc = func() time.Time {
			return time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
		}
		defer func() { nowFunc = origNow }()

		var capturedTime time.Time

		ctx := &ToolContext{
			TextbeltAPIKey: "test-key",
			ScheduleTool: func(toolName string, input map[string]any, scheduledFor time.Time, recurrence string) (string, error) {
				capturedTime = scheduledFor
				return "job-456", nil
			},
		}

		_, err := tool.Execute(map[string]any{
			"phone":   "5555555555",
			"message": "Later",
			"time":    "14:30",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedTime.Hour() != 14 || capturedTime.Minute() != 30 {
			t.Errorf("expected 14:30, got: %s", capturedTime.Format(time.RFC3339))
		}
	})
}

// ---------------------------------------------------------------------------
// parseScheduleTime
// ---------------------------------------------------------------------------

func TestParseScheduleTime(t *testing.T) {
	origNow := nowFunc
	defer func() { nowFunc = origNow }()

	t.Run("parses RFC3339", func(t *testing.T) {
		result, err := parseScheduleTime("2026-03-01T14:00:00Z")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Year() != 2026 || result.Month() != 3 || result.Day() != 1 {
			t.Errorf("unexpected date: %v", result)
		}
	})

	t.Run("parses HH:MM future today", func(t *testing.T) {
		nowFunc = func() time.Time {
			return time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
		}
		result, err := parseScheduleTime("14:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Hour() != 14 || result.Day() != 27 {
			t.Errorf("expected 14:00 on the 27th, got: %v", result)
		}
	})

	t.Run("parses HH:MM past rolls to tomorrow", func(t *testing.T) {
		nowFunc = func() time.Time {
			return time.Date(2026, 2, 27, 16, 0, 0, 0, time.UTC)
		}
		result, err := parseScheduleTime("14:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Day() != 28 {
			t.Errorf("expected rolled to 28th, got day: %d", result.Day())
		}
	})

	t.Run("rejects invalid format", func(t *testing.T) {
		_, err := parseScheduleTime("not-a-time")
		if err == nil {
			t.Fatal("expected error for invalid format")
		}
	})
}
