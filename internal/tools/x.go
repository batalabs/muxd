package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/provider"
)

const tweetMaxLen = 280

// xHTTPClient is overridable in tests.
var xHTTPClient = &http.Client{Timeout: 20 * time.Second}

// xPostURL is the base URL for creating a tweet (X API v2).
var xPostURL = "https://api.x.com/2/tweets"

// xSearchURL is the base URL for recent tweet search (X API v2).
var xSearchURL = "https://api.x.com/2/tweets/search/recent"

// xUsersMeURL is the URL for the authenticated user's profile (X API v2).
var xUsersMeURL = "https://api.x.com/2/users/me"

// xUsersMentionsURLFmt is the format string for fetching user mentions.
// Use fmt.Sprintf(xUsersMentionsURLFmt, userID).
var xUsersMentionsURLFmt = "https://api.x.com/2/users/%s/mentions"

// nowFunc is overridable in tests.
var nowFunc = time.Now

func xPostTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_post",
			Description: "Post a tweet via X API v2. Input text must be 1-280 characters.",
			Properties: map[string]provider.ToolProp{
				"text": {Type: "string", Description: "Tweet text, maximum 280 characters"},
			},
			Required: []string{"text"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			text, ok := input["text"].(string)
			if !ok {
				return "", fmt.Errorf("text is required")
			}
			token, tokenErr := resolveXPostTokenFromContext(ctx)
			if tokenErr != nil {
				return "", tokenErr
			}
			id, tweetURL, err := PostXTweet(text, token)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Posted tweet %s\n%s", id, tweetURL), nil
		},
	}
}

func xScheduleTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_schedule",
			Description: "Schedule a tweet for later posting. Requires scheduler support and a valid time.",
			Properties: map[string]provider.ToolProp{
				"text":       {Type: "string", Description: "Tweet text, maximum 280 characters"},
				"time":       {Type: "string", Description: "Schedule time: RFC3339 or HH:MM (local time)"},
				"recurrence": {Type: "string", Description: "Optional recurrence: once, daily, hourly"},
			},
			Required: []string{"text", "time"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || (ctx.ScheduleTool == nil && ctx.ScheduleTweet == nil) {
				return "", fmt.Errorf("X scheduler is not available in this runtime")
			}
			text, ok := input["text"].(string)
			if !ok {
				return "", fmt.Errorf("text is required")
			}
			if err := validateTweetText(text); err != nil {
				return "", err
			}
			timeRaw, ok := input["time"].(string)
			if !ok || strings.TrimSpace(timeRaw) == "" {
				return "", fmt.Errorf("time is required")
			}
			recurrence := "once"
			if v, ok := input["recurrence"].(string); ok && strings.TrimSpace(v) != "" {
				recurrence = strings.ToLower(strings.TrimSpace(v))
			}
			if recurrence != "once" && recurrence != "daily" && recurrence != "hourly" {
				return "", fmt.Errorf("recurrence must be one of: once, daily, hourly")
			}
			scheduledFor, err := ParseTweetScheduleTime(timeRaw, nowFunc())
			if err != nil {
				return "", err
			}
			var (
				id          string
				scheduleErr error
			)
			if ctx.ScheduleTool != nil {
				id, scheduleErr = ctx.ScheduleTool("x_post", map[string]any{"text": text}, scheduledFor, recurrence)
			} else {
				id, scheduleErr = ctx.ScheduleTweet(text, scheduledFor, recurrence)
			}
			if scheduleErr != nil {
				return "", scheduleErr
			}
			return fmt.Sprintf("Scheduled tweet %s for %s (%s)", id, scheduledFor.UTC().Format(time.RFC3339), recurrence), nil
		},
	}
}

// ParseTweetScheduleTime parses RFC3339 or HH:MM local time.
func ParseTweetScheduleTime(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("time is required")
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("15:04", raw); err == nil {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !candidate.After(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		return candidate.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q (use RFC3339 or HH:MM)", raw)
}

func xBearerFromEnv() string {
	return strings.TrimSpace(getEnvFunc("X_BEARER_TOKEN"))
}

func resolveXPostTokenFromContext(ctx *ToolContext) (string, error) {
	if ctx != nil {
		access := strings.TrimSpace(ctx.XAccessToken)
		expiry := parseXExpiry(ctx.XTokenExpiry)
		if access != "" && (expiry.IsZero() || time.Now().UTC().Before(expiry.Add(-60*time.Second))) {
			return access, nil
		}
		if strings.TrimSpace(ctx.XRefreshToken) != "" && strings.TrimSpace(ctx.XClientID) != "" {
			tok, err := RefreshXOAuthToken(ctx.XClientID, ctx.XClientSecret, ctx.XRefreshToken)
			if err == nil {
				saved := false
				if ctx.SaveXOAuthTokens != nil {
					if saveErr := ctx.SaveXOAuthTokens(tok.AccessToken, tok.RefreshToken, tok.ExpiresAt.UTC().Format(time.RFC3339)); saveErr != nil {
						fmt.Fprintf(os.Stderr, "x: save oauth tokens: %v\n", saveErr)
					} else {
						saved = true
					}
				}
				if !saved {
					fmt.Fprintf(os.Stderr, "x: warning: refreshed token was not persisted; you may need to re-authenticate next session\n")
				}
				return tok.AccessToken, nil
			}
			return "", fmt.Errorf("X OAuth token expired and refresh failed: %w. Run /x auth to re-authenticate", err)
		}
		if access != "" {
			return "", fmt.Errorf("X OAuth token expired and no refresh token available. Run /x auth to re-authenticate")
		}
	}
	bearer := xBearerFromEnv()
	if bearer != "" {
		return bearer, nil
	}
	return "", fmt.Errorf("X authentication not configured. Run /x auth to authenticate")
}

func validateTweetText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("tweet text cannot be empty")
	}
	if len([]rune(text)) > tweetMaxLen {
		return fmt.Errorf("tweet exceeds %d characters", tweetMaxLen)
	}
	return nil
}

// PostXTweet posts text using X API v2 and returns tweet ID + canonical URL.
func PostXTweet(text, bearerToken string) (string, string, error) {
	if err := validateTweetText(text); err != nil {
		return "", "", err
	}
	bearerToken = strings.TrimSpace(bearerToken)
	if bearerToken == "" {
		return "", "", fmt.Errorf("X authentication not configured. Run /x auth or set X_BEARER_TOKEN")
	}

	payload := map[string]string{"text": text}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, xPostURL, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("X API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading X API response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", "", mapXError(resp.StatusCode, respBody)
	}

	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", fmt.Errorf("parsing X API response: %w", err)
	}
	if strings.TrimSpace(out.Data.ID) == "" {
		return "", "", fmt.Errorf("X API response missing tweet id")
	}
	url := "https://x.com/i/web/status/" + out.Data.ID
	return out.Data.ID, url, nil
}

func xScheduleListTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_schedule_list",
			Description: "List pending scheduled X posts from the queue. Returns IDs, text previews, times, and recurrence.",
			Properties: map[string]provider.ToolProp{
				"limit": {Type: "integer", Description: "Maximum number of items to return (default: 20)"},
			},
			Required: []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.ListScheduledJobs == nil {
				return "", fmt.Errorf("schedule listing is not available in this runtime")
			}
			limit := 20
			if v, ok := input["limit"].(float64); ok && v > 0 {
				limit = int(v)
			}
			jobs, err := ctx.ListScheduledJobs("x_post", limit)
			if err != nil {
				return "", err
			}
			if len(jobs) == 0 {
				return "No scheduled X posts.", nil
			}
			var b strings.Builder
			for i, j := range jobs {
				idPrefix := j.ID
				if len(idPrefix) > 8 {
					idPrefix = idPrefix[:8]
				}
				text, _ := j.ToolInput["text"].(string)
				preview := text
				if len([]rune(preview)) > 60 {
					preview = string([]rune(preview)[:60]) + "..."
				}
				fmt.Fprintf(&b, "%d. [%s] %q — %s (%s) [%s]\n",
					i+1, idPrefix, preview,
					j.ScheduledFor.UTC().Format(time.RFC3339),
					j.Recurrence, j.Status)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

func xScheduleUpdateTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_schedule_update",
			Description: "Update a scheduled X post's text, time, or recurrence. At least one field must be provided.",
			Properties: map[string]provider.ToolProp{
				"id":         {Type: "string", Description: "Scheduled job ID (or 8-char prefix)"},
				"text":       {Type: "string", Description: "New tweet text (max 280 chars)"},
				"time":       {Type: "string", Description: "New schedule time: RFC3339 or HH:MM"},
				"recurrence": {Type: "string", Description: "New recurrence: once, daily, hourly"},
			},
			Required: []string{"id"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.UpdateScheduledJob == nil {
				return "", fmt.Errorf("schedule updating is not available in this runtime")
			}
			id, ok := input["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return "", fmt.Errorf("id is required")
			}

			var toolInput map[string]any
			var scheduledFor *time.Time
			var recurrence *string
			hasUpdate := false

			if text, ok := input["text"].(string); ok && strings.TrimSpace(text) != "" {
				if err := validateTweetText(text); err != nil {
					return "", err
				}
				toolInput = map[string]any{"text": text}
				hasUpdate = true
			}
			if timeRaw, ok := input["time"].(string); ok && strings.TrimSpace(timeRaw) != "" {
				t, err := ParseTweetScheduleTime(timeRaw, nowFunc())
				if err != nil {
					return "", err
				}
				scheduledFor = &t
				hasUpdate = true
			}
			if rec, ok := input["recurrence"].(string); ok && strings.TrimSpace(rec) != "" {
				rec = strings.ToLower(strings.TrimSpace(rec))
				if rec != "once" && rec != "daily" && rec != "hourly" {
					return "", fmt.Errorf("recurrence must be one of: once, daily, hourly")
				}
				recurrence = &rec
				hasUpdate = true
			}

			if !hasUpdate {
				return "", fmt.Errorf("at least one of text, time, or recurrence must be provided")
			}

			if err := ctx.UpdateScheduledJob(id, toolInput, scheduledFor, recurrence); err != nil {
				return "", err
			}

			var parts []string
			if toolInput != nil {
				parts = append(parts, "text")
			}
			if scheduledFor != nil {
				parts = append(parts, "time")
			}
			if recurrence != nil {
				parts = append(parts, "recurrence")
			}
			return fmt.Sprintf("Updated scheduled tweet %s (%s).", id, strings.Join(parts, ", ")), nil
		},
	}
}

func xScheduleCancelTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_schedule_cancel",
			Description: "Cancel a scheduled X post by ID.",
			Properties: map[string]provider.ToolProp{
				"id": {Type: "string", Description: "Scheduled job ID (or 8-char prefix)"},
			},
			Required: []string{"id"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.CancelScheduledJob == nil {
				return "", fmt.Errorf("schedule cancellation is not available in this runtime")
			}
			id, ok := input["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return "", fmt.Errorf("id is required")
			}
			if err := ctx.CancelScheduledJob(id); err != nil {
				return "", err
			}
			return fmt.Sprintf("Cancelled scheduled tweet %s.", id), nil
		},
	}
}

// ---------------------------------------------------------------------------
// x_search
// ---------------------------------------------------------------------------

func xSearchTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_search",
			Description: "Search recent tweets on X/Twitter. Returns matching tweets with author, text, date, URL, and engagement metrics.",
			Properties: map[string]provider.ToolProp{
				"query":       {Type: "string", Description: "Search query (X search syntax)"},
				"max_results": {Type: "integer", Description: "Number of results to return (10-100, default 10)"},
			},
			Required: []string{"query"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			query, ok := input["query"].(string)
			if !ok || strings.TrimSpace(query) == "" {
				return "", fmt.Errorf("query is required")
			}
			maxResults := 10
			if v, ok := input["max_results"].(float64); ok && v > 0 {
				maxResults = int(v)
			}
			if maxResults < 10 {
				maxResults = 10
			}
			if maxResults > 100 {
				maxResults = 100
			}
			token, err := resolveXPostTokenFromContext(ctx)
			if err != nil {
				return "", err
			}
			return SearchXTweets(query, maxResults, token)
		},
	}
}

// SearchXTweets searches recent tweets via X API v2 and returns formatted results.
func SearchXTweets(query string, maxResults int, bearerToken string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	bearerToken = strings.TrimSpace(bearerToken)
	if bearerToken == "" {
		return "", fmt.Errorf("X bearer token not set")
	}

	req, err := http.NewRequest(http.MethodGet, xSearchURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	q := req.URL.Query()
	q.Set("query", query)
	q.Set("max_results", fmt.Sprintf("%d", maxResults))
	q.Set("tweet.fields", "created_at,author_id,public_metrics")
	q.Set("expansions", "author_id")
	q.Set("user.fields", "username")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("X API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading X API response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", mapXError(resp.StatusCode, respBody)
	}

	var out xTweetListResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("parsing X API response: %w", err)
	}
	if len(out.Data) == 0 {
		return "No tweets found.", nil
	}
	return formatTweets(out), nil
}

// ---------------------------------------------------------------------------
// x_mentions
// ---------------------------------------------------------------------------

func xMentionsTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_mentions",
			Description: "Fetch recent mentions of the authenticated X/Twitter user. Returns tweets mentioning you with author, text, date, URL, and engagement metrics.",
			Properties: map[string]provider.ToolProp{
				"max_results": {Type: "integer", Description: "Number of results to return (5-100, default 10)"},
			},
			Required: []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			maxResults := 10
			if v, ok := input["max_results"].(float64); ok && v > 0 {
				maxResults = int(v)
			}
			if maxResults < 5 {
				maxResults = 5
			}
			if maxResults > 100 {
				maxResults = 100
			}
			token, err := resolveXPostTokenFromContext(ctx)
			if err != nil {
				return "", err
			}
			return GetXMentions(maxResults, token)
		},
	}
}

// GetXMentions fetches recent mentions of the authenticated user via X API v2.
func GetXMentions(maxResults int, bearerToken string) (string, error) {
	bearerToken = strings.TrimSpace(bearerToken)
	if bearerToken == "" {
		return "", fmt.Errorf("X bearer token not set")
	}

	// Step 1: get authenticated user ID.
	userID, err := getXAuthenticatedUserID(bearerToken)
	if err != nil {
		return "", fmt.Errorf("fetching authenticated user: %w", err)
	}

	// Step 2: fetch mentions.
	mentionsURL := fmt.Sprintf(xUsersMentionsURLFmt, userID)
	req, err := http.NewRequest(http.MethodGet, mentionsURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	q := req.URL.Query()
	q.Set("max_results", fmt.Sprintf("%d", maxResults))
	q.Set("tweet.fields", "created_at,author_id,public_metrics")
	q.Set("expansions", "author_id")
	q.Set("user.fields", "username")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("X API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading X API response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", mapXError(resp.StatusCode, respBody)
	}

	var out xTweetListResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("parsing X API response: %w", err)
	}
	if len(out.Data) == 0 {
		return "No mentions found.", nil
	}
	return formatTweets(out), nil
}

// getXAuthenticatedUserID fetches the authenticated user's ID via /2/users/me.
func getXAuthenticatedUserID(bearerToken string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, xUsersMeURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("X API request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", mapXError(resp.StatusCode, body)
	}

	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if strings.TrimSpace(out.Data.ID) == "" {
		return "", fmt.Errorf("X API returned empty user ID")
	}
	return out.Data.ID, nil
}

// ---------------------------------------------------------------------------
// x_reply
// ---------------------------------------------------------------------------

func xReplyTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "x_reply",
			Description: "Reply to a tweet on X/Twitter. Posts a reply to the specified tweet ID.",
			Properties: map[string]provider.ToolProp{
				"tweet_id": {Type: "string", Description: "The ID of the tweet to reply to"},
				"text":     {Type: "string", Description: "Reply text, maximum 280 characters"},
			},
			Required: []string{"tweet_id", "text"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			tweetID, ok := input["tweet_id"].(string)
			if !ok || strings.TrimSpace(tweetID) == "" {
				return "", fmt.Errorf("tweet_id is required")
			}
			text, ok := input["text"].(string)
			if !ok {
				return "", fmt.Errorf("text is required")
			}
			token, err := resolveXPostTokenFromContext(ctx)
			if err != nil {
				return "", err
			}
			id, replyURL, err := PostXReply(text, tweetID, token)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Replied with tweet %s\n%s", id, replyURL), nil
		},
	}
}

// PostXReply posts a reply to the specified tweet via X API v2.
func PostXReply(text, tweetID, bearerToken string) (string, string, error) {
	if err := validateTweetText(text); err != nil {
		return "", "", err
	}
	tweetID = strings.TrimSpace(tweetID)
	if tweetID == "" {
		return "", "", fmt.Errorf("tweet_id is required")
	}
	bearerToken = strings.TrimSpace(bearerToken)
	if bearerToken == "" {
		return "", "", fmt.Errorf("X bearer token not set")
	}

	payload := map[string]any{
		"text": text,
		"reply": map[string]string{
			"in_reply_to_tweet_id": tweetID,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, xPostURL, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("X API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading X API response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", "", mapXError(resp.StatusCode, respBody)
	}

	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", fmt.Errorf("parsing X API response: %w", err)
	}
	if strings.TrimSpace(out.Data.ID) == "" {
		return "", "", fmt.Errorf("X API response missing tweet id")
	}
	url := "https://x.com/i/web/status/" + out.Data.ID
	return out.Data.ID, url, nil
}

// ---------------------------------------------------------------------------
// Shared tweet formatting
// ---------------------------------------------------------------------------

// xTweetListResponse is the common response shape for search and mentions endpoints.
type xTweetListResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		AuthorID      string `json:"author_id"`
		CreatedAt     string `json:"created_at"`
		PublicMetrics struct {
			LikeCount    int `json:"like_count"`
			RetweetCount int `json:"retweet_count"`
			ReplyCount   int `json:"reply_count"`
		} `json:"public_metrics"`
	} `json:"data"`
	Includes struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	} `json:"includes"`
}

// formatTweets formats an xTweetListResponse into a numbered human-readable list.
func formatTweets(resp xTweetListResponse) string {
	userMap := make(map[string]string, len(resp.Includes.Users))
	for _, u := range resp.Includes.Users {
		userMap[u.ID] = u.Username
	}

	var b strings.Builder
	for i, tw := range resp.Data {
		username := userMap[tw.AuthorID]
		if username == "" {
			username = tw.AuthorID
		}
		text := tw.Text
		if len([]rune(text)) > 200 {
			text = string([]rune(text)[:200]) + "..."
		}
		date := tw.CreatedAt
		if t, err := time.Parse(time.RFC3339, date); err == nil {
			date = t.UTC().Format("2006-01-02 15:04")
		}
		url := "https://x.com/i/web/status/" + tw.ID
		fmt.Fprintf(&b, "%d. [@%s] %s (%s) — %s | %dL %dRT %dR\n",
			i+1, username, text, date, url,
			tw.PublicMetrics.LikeCount,
			tw.PublicMetrics.RetweetCount,
			tw.PublicMetrics.ReplyCount,
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func mapXError(status int, body []byte) error {
	bodyText := truncate(strings.TrimSpace(string(body)), 400)
	switch status {
	case http.StatusBadRequest:
		if strings.Contains(strings.ToLower(bodyText), "280") {
			return fmt.Errorf("tweet exceeds 280 characters")
		}
		return fmt.Errorf("X rejected request: %s", bodyText)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("X auth failed (HTTP %d): %s", status, bodyText)
	case http.StatusTooManyRequests:
		return fmt.Errorf("X rate limited this request. Try again later")
	default:
		return fmt.Errorf("X API error (HTTP %d): %s", status, bodyText)
	}
}
