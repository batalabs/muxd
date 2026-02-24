package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// web_search — Brave Search API
// ---------------------------------------------------------------------------

func webSearchTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "web_search",
			Description: "Search the web using the Brave Search API. Returns a list of results with title, URL, and snippet. Requires the BRAVE_SEARCH_API_KEY environment variable. Use this to find current information, documentation, or answers to questions.",
			Properties: map[string]provider.ToolProp{
				"query": {Type: "string", Description: "Search query"},
				"count": {Type: "integer", Description: "Number of results to return (default: 5, max: 20)"},
			},
			Required: []string{"query"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			query, ok := input["query"].(string)
			if !ok || query == "" {
				return "", fmt.Errorf("query is required")
			}

			count := 5
			if v, ok := input["count"].(float64); ok && v > 0 {
				count = int(v)
				if count > 20 {
					count = 20
				}
			}

			var ctxKey string
			if ctx != nil {
				ctxKey = ctx.BraveAPIKey
			}
			return braveSearch(query, count, ctxKey)
		},
	}
}

// braveSearchHTTPClient is overridable in tests.
var braveSearchHTTPClient = &http.Client{Timeout: 15 * time.Second}

// braveSearchURL is the base URL for the Brave Search API. Override in tests.
var braveSearchURL = "https://api.search.brave.com/res/v1/web/search"

// getEnvFunc allows overriding os.Getenv in tests.
var getEnvFunc = os.Getenv

// braveSearch calls the Brave Search API and returns formatted results.
// It checks the env var first, then falls back to the provided config key.
func braveSearch(query string, count int, configKey string) (string, error) {
	apiKey := getEnvFunc("BRAVE_SEARCH_API_KEY")
	if apiKey == "" {
		apiKey = configKey
	}
	if apiKey == "" {
		return "", fmt.Errorf("BRAVE_SEARCH_API_KEY not set. Use /config set brave.api_key <key> or set the BRAVE_SEARCH_API_KEY environment variable.")
	}

	req, err := http.NewRequest(http.MethodGet, braveSearchURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	q := req.URL.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", count))
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := braveSearchHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Brave Search API error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result braveSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Web.Results) == 0 {
		return "No results found.", nil
	}

	var b strings.Builder
	for i, r := range result.Web.Results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if r.Description != "" {
			fmt.Fprintf(&b, "   %s\n", r.Description)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// ---------------------------------------------------------------------------
// web_fetch — HTTP GET with HTML-to-text extraction
// ---------------------------------------------------------------------------

func webFetchTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "web_fetch",
			Description: "Fetch a URL and return the text content. HTML is stripped to plain text. Output is truncated at 50KB. Use this to read documentation pages, API responses, or any web content.",
			Properties: map[string]provider.ToolProp{
				"url": {Type: "string", Description: "URL to fetch"},
			},
			Required: []string{"url"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			rawURL, ok := input["url"].(string)
			if !ok || rawURL == "" {
				return "", fmt.Errorf("url is required")
			}

			return fetchAndExtractText(rawURL)
		},
	}
}

// webFetchHTTPClient is overridable in tests.
var webFetchHTTPClient = &http.Client{Timeout: 30 * time.Second}

// fetchAndExtractText fetches a URL and returns the text content.
func fetchAndExtractText(rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "muxd/1.0 (coding assistant)")

	resp, err := webFetchHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, rawURL)
	}

	// Read up to 1MB of raw content.
	const maxRead = 1024 * 1024
	limited := io.LimitReader(resp.Body, maxRead)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	content := string(data)

	// If the content looks like HTML, extract text.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "html") || strings.HasPrefix(strings.TrimSpace(content), "<") {
		content = htmlToText(content)
	}

	// Truncate output.
	const maxOutput = 50 * 1024
	if len(content) > maxOutput {
		content = content[:maxOutput] + "\n... (truncated at 50KB)"
	}

	return content, nil
}

// htmlToText strips HTML tags and extracts plain text.
func htmlToText(html string) string {
	var b strings.Builder
	inTag := false
	inScript := false
	inStyle := false
	lastSpace := false

	// Pre-process: replace common HTML entities.
	html = strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&nbsp;", " ",
	).Replace(html)

	lower := strings.ToLower(html)

	for i := 0; i < len(html); i++ {
		ch := html[i]

		if ch == '<' {
			rest := lower[i:]
			if strings.HasPrefix(rest, "<script") {
				inScript = true
			} else if strings.HasPrefix(rest, "</script") {
				inScript = false
			} else if strings.HasPrefix(rest, "<style") {
				inStyle = true
			} else if strings.HasPrefix(rest, "</style") {
				inStyle = false
			}

			// Block-level tags → newline.
			for _, tag := range []string{"<br", "<p ", "<p>", "</p>", "<div", "</div>", "<h1", "<h2", "<h3", "<h4", "<h5", "<h6", "</h1", "</h2", "</h3", "</h4", "</h5", "</h6", "<li", "<tr"} {
				if strings.HasPrefix(rest, tag) {
					b.WriteByte('\n')
					lastSpace = true
					break
				}
			}

			inTag = true
			continue
		}

		if ch == '>' {
			inTag = false
			continue
		}

		if inTag || inScript || inStyle {
			continue
		}

		// Collapse whitespace.
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}

		b.WriteByte(ch)
		lastSpace = false
	}

	// Collapse multiple newlines.
	result := b.String()
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

// truncate returns s trimmed to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
