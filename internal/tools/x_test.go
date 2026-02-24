package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// validateTweetText
// ---------------------------------------------------------------------------

func TestValidateTweetText(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
		errMsg  string
	}{
		{"valid short", "hello", false, ""},
		{"exactly 280", strings.Repeat("a", 280), false, ""},
		{"empty", "", true, "empty"},
		{"whitespace only", "   ", true, "empty"},
		{"exceeds 280", strings.Repeat("x", 281), true, "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTweetText(tt.text)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatTweets
// ---------------------------------------------------------------------------

func TestFormatTweets(t *testing.T) {
	t.Run("formats tweets with user mapping", func(t *testing.T) {
		resp := xTweetListResponse{
			Data: []struct {
				ID            string `json:"id"`
				Text          string `json:"text"`
				AuthorID      string `json:"author_id"`
				CreatedAt     string `json:"created_at"`
				PublicMetrics struct {
					LikeCount    int `json:"like_count"`
					RetweetCount int `json:"retweet_count"`
					ReplyCount   int `json:"reply_count"`
				} `json:"public_metrics"`
			}{
				{
					ID:        "1",
					Text:      "Hello world",
					AuthorID:  "u1",
					CreatedAt: "2026-02-20T12:00:00Z",
				},
			},
		}
		resp.Data[0].PublicMetrics.LikeCount = 5
		resp.Data[0].PublicMetrics.RetweetCount = 2
		resp.Data[0].PublicMetrics.ReplyCount = 1
		resp.Includes.Users = []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		}{{ID: "u1", Username: "testuser"}}

		result := formatTweets(resp)
		if !strings.Contains(result, "@testuser") {
			t.Errorf("expected @testuser, got: %q", result)
		}
		if !strings.Contains(result, "Hello world") {
			t.Errorf("expected tweet text, got: %q", result)
		}
		if !strings.Contains(result, "5L") {
			t.Errorf("expected like count, got: %q", result)
		}
		if !strings.Contains(result, "2RT") {
			t.Errorf("expected retweet count, got: %q", result)
		}
		if !strings.Contains(result, "1R") {
			t.Errorf("expected reply count, got: %q", result)
		}
	})

	t.Run("falls back to author_id when no user mapping", func(t *testing.T) {
		resp := xTweetListResponse{
			Data: []struct {
				ID            string `json:"id"`
				Text          string `json:"text"`
				AuthorID      string `json:"author_id"`
				CreatedAt     string `json:"created_at"`
				PublicMetrics struct {
					LikeCount    int `json:"like_count"`
					RetweetCount int `json:"retweet_count"`
					ReplyCount   int `json:"reply_count"`
				} `json:"public_metrics"`
			}{
				{ID: "1", Text: "test", AuthorID: "unknown_id", CreatedAt: "invalid-date"},
			},
		}
		result := formatTweets(resp)
		if !strings.Contains(result, "@unknown_id") {
			t.Errorf("expected fallback to author_id, got: %q", result)
		}
	})

	t.Run("truncates long tweet text", func(t *testing.T) {
		longText := strings.Repeat("x", 250)
		resp := xTweetListResponse{
			Data: []struct {
				ID            string `json:"id"`
				Text          string `json:"text"`
				AuthorID      string `json:"author_id"`
				CreatedAt     string `json:"created_at"`
				PublicMetrics struct {
					LikeCount    int `json:"like_count"`
					RetweetCount int `json:"retweet_count"`
					ReplyCount   int `json:"reply_count"`
				} `json:"public_metrics"`
			}{
				{ID: "1", Text: longText, AuthorID: "u1", CreatedAt: "2026-02-20T12:00:00Z"},
			},
		}
		result := formatTweets(resp)
		if !strings.Contains(result, "...") {
			t.Errorf("expected truncation, got length %d", len(result))
		}
	})
}

// ---------------------------------------------------------------------------
// mapXError
// ---------------------------------------------------------------------------

func TestMapXError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"bad request with 280", 400, "tweet exceeds 280 characters limit", "exceeds 280"},
		{"bad request generic", 400, "some other error", "rejected request"},
		{"unauthorized", 401, "bad token", "auth failed"},
		{"forbidden", 403, "not allowed", "auth failed"},
		{"rate limited", 429, "too many", "rate limited"},
		{"server error", 500, "internal error", "API error (HTTP 500)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapXError(tt.status, []byte(tt.body))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Errorf("mapXError(%d) = %q, want to contain %q", tt.status, err.Error(), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PostXTweet
// ---------------------------------------------------------------------------

func TestPostXTweet(t *testing.T) {
	origURL := xPostURL
	origClient := xHTTPClient
	t.Cleanup(func() {
		xPostURL = origURL
		xHTTPClient = origClient
	})

	t.Run("posts tweet successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("authorization header = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":{"id":"12345","text":"hello"}}`)
		}))
		defer srv.Close()

		xPostURL = srv.URL
		xHTTPClient = srv.Client()

		id, url, err := PostXTweet("hello world", "test-token")
		if err != nil {
			t.Fatalf("PostXTweet error: %v", err)
		}
		if id != "12345" {
			t.Fatalf("id = %q, want %q", id, "12345")
		}
		if !strings.HasSuffix(url, "/12345") {
			t.Fatalf("url = %q", url)
		}
	})

	t.Run("returns validation error for long tweet", func(t *testing.T) {
		long := strings.Repeat("x", 281)
		_, _, err := PostXTweet(long, "test-token")
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected length error, got: %v", err)
		}
	})

	t.Run("maps auth errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"title":"Unauthorized"}`)
		}))
		defer srv.Close()
		xPostURL = srv.URL
		xHTTPClient = srv.Client()

		_, _, err := PostXTweet("hello", "bad-token")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "auth failed") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})

	t.Run("maps rate limit errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"title":"Too many requests"}`)
		}))
		defer srv.Close()
		xPostURL = srv.URL
		xHTTPClient = srv.Client()

		_, _, err := PostXTweet("hello", "ok")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate limited") {
			t.Fatalf("expected rate limit error, got: %v", err)
		}
	})
}

func TestXScheduleTool(t *testing.T) {
	tool := xScheduleTool()

	t.Run("schedules tweet through callback", func(t *testing.T) {
		var gotText string
		var gotRecurrence string
		var gotWhen time.Time
		var gotTool string
		ctx := &ToolContext{
			ScheduleTool: func(toolName string, input map[string]any, scheduledFor time.Time, recurrence string) (string, error) {
				gotTool = toolName
				gotText, _ = input["text"].(string)
				gotWhen = scheduledFor
				gotRecurrence = recurrence
				return "sched-1", nil
			},
		}
		out, err := tool.Execute(map[string]any{
			"text":       "ship it",
			"time":       "23:59",
			"recurrence": "daily",
		}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !strings.Contains(out, "sched-1") {
			t.Fatalf("unexpected output: %q", out)
		}
		if gotText != "ship it" {
			t.Fatalf("text = %q", gotText)
		}
		if gotTool != "x_post" {
			t.Fatalf("toolName = %q", gotTool)
		}
		if gotRecurrence != "daily" {
			t.Fatalf("recurrence = %q", gotRecurrence)
		}
		if gotWhen.IsZero() {
			t.Fatal("scheduled time was zero")
		}
	})

	t.Run("fails when scheduler callback missing", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"text": "ship it",
			"time": "2026-02-21T12:00:00Z",
		}, &ToolContext{})
		if err == nil || !strings.Contains(err.Error(), "not available") {
			t.Fatalf("expected scheduler unavailable error, got: %v", err)
		}
	})
}

func TestXScheduleListTool(t *testing.T) {
	tool := xScheduleListTool()

	t.Run("returns formatted list", func(t *testing.T) {
		ctx := &ToolContext{
			ListScheduledJobs: func(toolName string, limit int) ([]ScheduledJobInfo, error) {
				if toolName != "x_post" {
					t.Fatalf("expected toolName x_post, got %q", toolName)
				}
				return []ScheduledJobInfo{
					{
						ID:           "abcdef12-3456-7890-abcd-ef1234567890",
						ToolName:     "x_post",
						ToolInput:    map[string]any{"text": "hello world from muxd"},
						ScheduledFor: time.Date(2026, 2, 21, 18, 30, 0, 0, time.UTC),
						Recurrence:   "daily",
						Status:       "pending",
						CreatedAt:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
					},
				}, nil
			},
		}
		out, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !strings.Contains(out, "abcdef12") {
			t.Errorf("expected ID prefix in output, got: %q", out)
		}
		if !strings.Contains(out, "hello world from muxd") {
			t.Errorf("expected text preview in output, got: %q", out)
		}
		if !strings.Contains(out, "daily") {
			t.Errorf("expected recurrence in output, got: %q", out)
		}
	})

	t.Run("returns empty message", func(t *testing.T) {
		ctx := &ToolContext{
			ListScheduledJobs: func(toolName string, limit int) ([]ScheduledJobInfo, error) {
				return nil, nil
			},
		}
		out, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if out != "No scheduled X posts." {
			t.Errorf("expected empty message, got: %q", out)
		}
	})

	t.Run("fails without callback", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{}, &ToolContext{})
		if err == nil || !strings.Contains(err.Error(), "not available") {
			t.Fatalf("expected unavailable error, got: %v", err)
		}
	})
}

func TestXScheduleUpdateTool(t *testing.T) {
	tool := xScheduleUpdateTool()

	t.Run("updates text", func(t *testing.T) {
		var gotID string
		var gotInput map[string]any
		ctx := &ToolContext{
			UpdateScheduledJob: func(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error {
				gotID = id
				gotInput = toolInput
				return nil
			},
		}
		out, err := tool.Execute(map[string]any{
			"id":   "abc123",
			"text": "updated tweet",
		}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if gotID != "abc123" {
			t.Errorf("id = %q, want %q", gotID, "abc123")
		}
		if gotInput["text"] != "updated tweet" {
			t.Errorf("text = %q", gotInput["text"])
		}
		if !strings.Contains(out, "Updated") {
			t.Errorf("expected Updated in output, got: %q", out)
		}
	})

	t.Run("rejects invalid recurrence", func(t *testing.T) {
		ctx := &ToolContext{
			UpdateScheduledJob: func(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error {
				return nil
			},
		}
		_, err := tool.Execute(map[string]any{
			"id":         "abc",
			"recurrence": "weekly",
		}, ctx)
		if err == nil || !strings.Contains(err.Error(), "recurrence") {
			t.Fatalf("expected recurrence error, got: %v", err)
		}
	})

	t.Run("rejects long tweet text", func(t *testing.T) {
		ctx := &ToolContext{
			UpdateScheduledJob: func(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error {
				return nil
			},
		}
		_, err := tool.Execute(map[string]any{
			"id":   "abc",
			"text": strings.Repeat("x", 281),
		}, ctx)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected length error, got: %v", err)
		}
	})

	t.Run("requires at least one update field", func(t *testing.T) {
		ctx := &ToolContext{
			UpdateScheduledJob: func(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error {
				return nil
			},
		}
		_, err := tool.Execute(map[string]any{
			"id": "abc",
		}, ctx)
		if err == nil || !strings.Contains(err.Error(), "at least one") {
			t.Fatalf("expected 'at least one' error, got: %v", err)
		}
	})

	t.Run("fails without callback", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"id":   "abc",
			"text": "hello",
		}, &ToolContext{})
		if err == nil || !strings.Contains(err.Error(), "not available") {
			t.Fatalf("expected unavailable error, got: %v", err)
		}
	})
}

func TestXScheduleCancelTool(t *testing.T) {
	tool := xScheduleCancelTool()

	t.Run("cancels scheduled tweet", func(t *testing.T) {
		var gotID string
		ctx := &ToolContext{
			CancelScheduledJob: func(id string) error {
				gotID = id
				return nil
			},
		}
		out, err := tool.Execute(map[string]any{
			"id": "abc123",
		}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if gotID != "abc123" {
			t.Errorf("id = %q, want %q", gotID, "abc123")
		}
		if !strings.Contains(out, "Cancelled") {
			t.Errorf("expected Cancelled in output, got: %q", out)
		}
	})

	t.Run("requires id", func(t *testing.T) {
		ctx := &ToolContext{
			CancelScheduledJob: func(id string) error {
				return nil
			},
		}
		_, err := tool.Execute(map[string]any{}, ctx)
		if err == nil || !strings.Contains(err.Error(), "id is required") {
			t.Fatalf("expected id required error, got: %v", err)
		}
	})

	t.Run("fails without callback", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"id": "abc",
		}, &ToolContext{})
		if err == nil || !strings.Contains(err.Error(), "not available") {
			t.Fatalf("expected unavailable error, got: %v", err)
		}
	})
}

func TestSearchXTweets(t *testing.T) {
	origSearchURL := xSearchURL
	origClient := xHTTPClient
	t.Cleanup(func() {
		xSearchURL = origSearchURL
		xHTTPClient = origClient
	})

	t.Run("returns formatted search results", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatalf("method = %q, want GET", r.Method)
			}
			if got := r.URL.Query().Get("query"); got != "golang" {
				t.Fatalf("query = %q, want %q", got, "golang")
			}
			if got := r.URL.Query().Get("max_results"); got != "10" {
				t.Fatalf("max_results = %q, want %q", got, "10")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{
				"data": [{"id":"111","text":"Go is great","author_id":"u1","created_at":"2026-02-20T12:00:00Z","public_metrics":{"like_count":5,"retweet_count":2,"reply_count":1}}],
				"includes":{"users":[{"id":"u1","username":"gopher"}]}
			}`)
		}))
		defer srv.Close()
		xSearchURL = srv.URL
		xHTTPClient = srv.Client()

		result, err := SearchXTweets("golang", 10, "test-token")
		if err != nil {
			t.Fatalf("SearchXTweets error: %v", err)
		}
		if !strings.Contains(result, "@gopher") {
			t.Errorf("expected @gopher in result, got: %q", result)
		}
		if !strings.Contains(result, "Go is great") {
			t.Errorf("expected tweet text in result, got: %q", result)
		}
		if !strings.Contains(result, "5L") {
			t.Errorf("expected like count in result, got: %q", result)
		}
		if !strings.Contains(result, "2RT") {
			t.Errorf("expected retweet count in result, got: %q", result)
		}
	})

	t.Run("returns no tweets found for empty results", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":[],"includes":{}}`)
		}))
		defer srv.Close()
		xSearchURL = srv.URL
		xHTTPClient = srv.Client()

		result, err := SearchXTweets("noresultsquery", 10, "test-token")
		if err != nil {
			t.Fatalf("SearchXTweets error: %v", err)
		}
		if result != "No tweets found." {
			t.Errorf("expected empty message, got: %q", result)
		}
	})

	t.Run("maps API errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"title":"Too many requests"}`)
		}))
		defer srv.Close()
		xSearchURL = srv.URL
		xHTTPClient = srv.Client()

		_, err := SearchXTweets("test", 10, "test-token")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate limited") {
			t.Fatalf("expected rate limit error, got: %v", err)
		}
	})

	t.Run("returns error for empty query", func(t *testing.T) {
		_, err := SearchXTweets("", 10, "test-token")
		if err == nil || !strings.Contains(err.Error(), "query is required") {
			t.Fatalf("expected query required error, got: %v", err)
		}
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		_, err := SearchXTweets("test", 10, "")
		if err == nil || !strings.Contains(err.Error(), "bearer token") {
			t.Fatalf("expected bearer token error, got: %v", err)
		}
	})
}

func TestGetXMentions(t *testing.T) {
	origMeURL := xUsersMeURL
	origMentionsFmt := xUsersMentionsURLFmt
	origClient := xHTTPClient
	t.Cleanup(func() {
		xUsersMeURL = origMeURL
		xUsersMentionsURLFmt = origMentionsFmt
		xHTTPClient = origClient
	})

	t.Run("returns formatted mentions", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			if callCount == 1 {
				// /users/me
				_, _ = fmt.Fprint(w, `{"data":{"id":"user123"}}`)
				return
			}
			// /users/user123/mentions
			if !strings.Contains(r.URL.Path, "user123") {
				t.Fatalf("expected user123 in path, got: %s", r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{
				"data": [{"id":"222","text":"Hey @you check this","author_id":"u2","created_at":"2026-02-20T14:00:00Z","public_metrics":{"like_count":3,"retweet_count":0,"reply_count":1}}],
				"includes":{"users":[{"id":"u2","username":"mentioner"}]}
			}`)
		}))
		defer srv.Close()
		xUsersMeURL = srv.URL
		xUsersMentionsURLFmt = srv.URL + "/users/%s/mentions"
		xHTTPClient = srv.Client()

		result, err := GetXMentions(10, "test-token")
		if err != nil {
			t.Fatalf("GetXMentions error: %v", err)
		}
		if !strings.Contains(result, "@mentioner") {
			t.Errorf("expected @mentioner in result, got: %q", result)
		}
		if !strings.Contains(result, "Hey @you check this") {
			t.Errorf("expected tweet text in result, got: %q", result)
		}
		if callCount != 2 {
			t.Errorf("expected 2 API calls, got %d", callCount)
		}
	})

	t.Run("returns no mentions found for empty results", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			if callCount == 1 {
				_, _ = fmt.Fprint(w, `{"data":{"id":"user123"}}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"data":[],"includes":{}}`)
		}))
		defer srv.Close()
		xUsersMeURL = srv.URL
		xUsersMentionsURLFmt = srv.URL + "/users/%s/mentions"
		xHTTPClient = srv.Client()

		result, err := GetXMentions(10, "test-token")
		if err != nil {
			t.Fatalf("GetXMentions error: %v", err)
		}
		if result != "No mentions found." {
			t.Errorf("expected empty message, got: %q", result)
		}
	})

	t.Run("returns error when /users/me fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"title":"Unauthorized"}`)
		}))
		defer srv.Close()
		xUsersMeURL = srv.URL
		xHTTPClient = srv.Client()

		_, err := GetXMentions(10, "bad-token")
		if err == nil || !strings.Contains(err.Error(), "authenticated user") {
			t.Fatalf("expected authenticated user error, got: %v", err)
		}
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		_, err := GetXMentions(10, "")
		if err == nil || !strings.Contains(err.Error(), "bearer token") {
			t.Fatalf("expected bearer token error, got: %v", err)
		}
	})
}

func TestPostXReply(t *testing.T) {
	origURL := xPostURL
	origClient := xHTTPClient
	t.Cleanup(func() {
		xPostURL = origURL
		xHTTPClient = origClient
	})

	t.Run("posts reply successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %q, want POST", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("authorization header = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":{"id":"99999"}}`)
		}))
		defer srv.Close()
		xPostURL = srv.URL
		xHTTPClient = srv.Client()

		id, url, err := PostXReply("nice post!", "12345", "test-token")
		if err != nil {
			t.Fatalf("PostXReply error: %v", err)
		}
		if id != "99999" {
			t.Fatalf("id = %q, want %q", id, "99999")
		}
		if !strings.HasSuffix(url, "/99999") {
			t.Fatalf("url = %q", url)
		}
	})

	t.Run("returns validation error for empty text", func(t *testing.T) {
		_, _, err := PostXReply("", "12345", "test-token")
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected empty text error, got: %v", err)
		}
	})

	t.Run("returns validation error for long text", func(t *testing.T) {
		long := strings.Repeat("x", 281)
		_, _, err := PostXReply(long, "12345", "test-token")
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected length error, got: %v", err)
		}
	})

	t.Run("returns error for empty tweet_id", func(t *testing.T) {
		_, _, err := PostXReply("hello", "", "test-token")
		if err == nil || !strings.Contains(err.Error(), "tweet_id") {
			t.Fatalf("expected tweet_id error, got: %v", err)
		}
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		_, _, err := PostXReply("hello", "12345", "")
		if err == nil || !strings.Contains(err.Error(), "bearer token") {
			t.Fatalf("expected bearer token error, got: %v", err)
		}
	})

	t.Run("maps API errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"title":"Forbidden"}`)
		}))
		defer srv.Close()
		xPostURL = srv.URL
		xHTTPClient = srv.Client()

		_, _, err := PostXReply("hello", "12345", "test-token")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "auth failed") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})
}

func TestXSearchTool(t *testing.T) {
	origSearchURL := xSearchURL
	origClient := xHTTPClient
	t.Cleanup(func() {
		xSearchURL = origSearchURL
		xHTTPClient = origClient
	})

	tool := xSearchTool()

	t.Run("executes search via tool", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{
				"data": [{"id":"333","text":"tool test","author_id":"u3","created_at":"2026-02-20T16:00:00Z","public_metrics":{"like_count":1,"retweet_count":0,"reply_count":0}}],
				"includes":{"users":[{"id":"u3","username":"tooluser"}]}
			}`)
		}))
		defer srv.Close()
		xSearchURL = srv.URL
		xHTTPClient = srv.Client()

		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		out, err := tool.Execute(map[string]any{"query": "test"}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !strings.Contains(out, "@tooluser") {
			t.Errorf("expected @tooluser in output, got: %q", out)
		}
	})

	t.Run("returns error for missing query", func(t *testing.T) {
		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		_, err := tool.Execute(map[string]any{}, ctx)
		if err == nil || !strings.Contains(err.Error(), "query is required") {
			t.Fatalf("expected query error, got: %v", err)
		}
	})

	t.Run("clamps max_results", func(t *testing.T) {
		var gotMaxResults string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMaxResults = r.URL.Query().Get("max_results")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":[],"includes":{}}`)
		}))
		defer srv.Close()
		xSearchURL = srv.URL
		xHTTPClient = srv.Client()

		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		_, _ = tool.Execute(map[string]any{"query": "test", "max_results": float64(200)}, ctx)
		if gotMaxResults != "100" {
			t.Errorf("max_results = %q, want %q", gotMaxResults, "100")
		}
	})
}

func TestXMentionsTool(t *testing.T) {
	origMeURL := xUsersMeURL
	origMentionsFmt := xUsersMentionsURLFmt
	origClient := xHTTPClient
	t.Cleanup(func() {
		xUsersMeURL = origMeURL
		xUsersMentionsURLFmt = origMentionsFmt
		xHTTPClient = origClient
	})

	tool := xMentionsTool()

	t.Run("executes mentions via tool", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			if callCount == 1 {
				_, _ = fmt.Fprint(w, `{"data":{"id":"myid"}}`)
				return
			}
			_, _ = fmt.Fprint(w, `{
				"data": [{"id":"444","text":"hey there","author_id":"u4","created_at":"2026-02-20T18:00:00Z","public_metrics":{"like_count":0,"retweet_count":0,"reply_count":0}}],
				"includes":{"users":[{"id":"u4","username":"friend"}]}
			}`)
		}))
		defer srv.Close()
		xUsersMeURL = srv.URL
		xUsersMentionsURLFmt = srv.URL + "/users/%s/mentions"
		xHTTPClient = srv.Client()

		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		out, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !strings.Contains(out, "@friend") {
			t.Errorf("expected @friend in output, got: %q", out)
		}
	})

	t.Run("clamps low max_results", func(t *testing.T) {
		var gotMaxResults string
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			if callCount == 1 {
				_, _ = fmt.Fprint(w, `{"data":{"id":"myid"}}`)
				return
			}
			gotMaxResults = r.URL.Query().Get("max_results")
			_, _ = fmt.Fprint(w, `{"data":[],"includes":{}}`)
		}))
		defer srv.Close()
		xUsersMeURL = srv.URL
		xUsersMentionsURLFmt = srv.URL + "/users/%s/mentions"
		xHTTPClient = srv.Client()

		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		_, _ = tool.Execute(map[string]any{"max_results": float64(2)}, ctx)
		if gotMaxResults != "5" {
			t.Errorf("max_results = %q, want %q", gotMaxResults, "5")
		}
	})
}

func TestXReplyTool(t *testing.T) {
	origURL := xPostURL
	origClient := xHTTPClient
	t.Cleanup(func() {
		xPostURL = origURL
		xHTTPClient = origClient
	})

	tool := xReplyTool()

	t.Run("executes reply via tool", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":{"id":"55555"}}`)
		}))
		defer srv.Close()
		xPostURL = srv.URL
		xHTTPClient = srv.Client()

		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		out, err := tool.Execute(map[string]any{
			"tweet_id": "12345",
			"text":     "great tweet!",
		}, ctx)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !strings.Contains(out, "55555") {
			t.Errorf("expected reply ID in output, got: %q", out)
		}
		if !strings.Contains(out, "Replied") {
			t.Errorf("expected Replied in output, got: %q", out)
		}
	})

	t.Run("returns error for missing tweet_id", func(t *testing.T) {
		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		_, err := tool.Execute(map[string]any{"text": "hello"}, ctx)
		if err == nil || !strings.Contains(err.Error(), "tweet_id") {
			t.Fatalf("expected tweet_id error, got: %v", err)
		}
	})

	t.Run("returns error for missing text", func(t *testing.T) {
		ctx := &ToolContext{XAccessToken: "test-token", XTokenExpiry: "2099-01-01T00:00:00Z"}
		_, err := tool.Execute(map[string]any{"tweet_id": "12345"}, ctx)
		if err == nil || !strings.Contains(err.Error(), "text is required") {
			t.Fatalf("expected text required error, got: %v", err)
		}
	})
}

func TestParseTweetScheduleTime(t *testing.T) {
	now := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)

	t.Run("parses rfc3339", func(t *testing.T) {
		got, err := ParseTweetScheduleTime("2026-02-21T09:15:00Z", now)
		if err != nil {
			t.Fatalf("ParseTweetScheduleTime error: %v", err)
		}
		if got.Format(time.RFC3339) != "2026-02-21T09:15:00Z" {
			t.Fatalf("got %s", got.Format(time.RFC3339))
		}
	})

	t.Run("parses hhmm", func(t *testing.T) {
		got, err := ParseTweetScheduleTime("11:30", now)
		if err != nil {
			t.Fatalf("ParseTweetScheduleTime error: %v", err)
		}
		if got.Hour() != 11 || got.Minute() != 30 {
			t.Fatalf("unexpected parsed time: %s", got.Format(time.RFC3339))
		}
	})
}
