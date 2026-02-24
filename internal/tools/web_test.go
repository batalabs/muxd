package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// web_search
// ---------------------------------------------------------------------------

func TestWebSearchTool(t *testing.T) {
	tool := webSearchTool()

	t.Run("missing API key returns error", func(t *testing.T) {
		origGetEnv := getEnvFunc
		getEnvFunc = func(key string) string { return "" }
		defer func() { getEnvFunc = origGetEnv }()

		_, err := tool.Execute(map[string]any{"query": "golang"}, nil)
		if err == nil {
			t.Fatal("expected error for missing API key")
		}
		if !strings.Contains(err.Error(), "BRAVE_SEARCH_API_KEY") {
			t.Errorf("expected API key error, got: %v", err)
		}
	})

	t.Run("returns formatted results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Subscription-Token") != "test-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			q := r.URL.Query().Get("q")
			if q == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"web": map[string]any{
					"results": []map[string]any{
						{
							"title":       "Go Programming Language",
							"url":         "https://go.dev",
							"description": "Build fast, reliable software.",
						},
						{
							"title":       "Go Wiki",
							"url":         "https://go.dev/wiki",
							"description": "Community wiki.",
						},
					},
				},
			})
		}))
		defer server.Close()

		origURL := braveSearchURL
		origClient := braveSearchHTTPClient
		origGetEnv := getEnvFunc
		braveSearchURL = server.URL
		braveSearchHTTPClient = server.Client()
		getEnvFunc = func(key string) string {
			if key == "BRAVE_SEARCH_API_KEY" {
				return "test-key"
			}
			return ""
		}
		defer func() {
			braveSearchURL = origURL
			braveSearchHTTPClient = origClient
			getEnvFunc = origGetEnv
		}()

		result, err := tool.Execute(map[string]any{"query": "golang"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Go Programming Language") {
			t.Errorf("expected title in result, got: %s", result)
		}
		if !strings.Contains(result, "https://go.dev") {
			t.Errorf("expected URL in result, got: %s", result)
		}
		if !strings.Contains(result, "Build fast") {
			t.Errorf("expected description in result, got: %s", result)
		}
	})

	t.Run("no results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"web": map[string]any{
					"results": []map[string]any{},
				},
			})
		}))
		defer server.Close()

		origURL := braveSearchURL
		origClient := braveSearchHTTPClient
		origGetEnv := getEnvFunc
		braveSearchURL = server.URL
		braveSearchHTTPClient = server.Client()
		getEnvFunc = func(key string) string { return "test-key" }
		defer func() {
			braveSearchURL = origURL
			braveSearchHTTPClient = origClient
			getEnvFunc = origGetEnv
		}()

		result, err := tool.Execute(map[string]any{"query": "xyznonexistent"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "No results found." {
			t.Errorf("expected no results message, got: %s", result)
		}
	})

	t.Run("API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited"))
		}))
		defer server.Close()

		origURL := braveSearchURL
		origClient := braveSearchHTTPClient
		origGetEnv := getEnvFunc
		braveSearchURL = server.URL
		braveSearchHTTPClient = server.Client()
		getEnvFunc = func(key string) string { return "test-key" }
		defer func() {
			braveSearchURL = origURL
			braveSearchHTTPClient = origClient
			getEnvFunc = origGetEnv
		}()

		_, err := tool.Execute(map[string]any{"query": "test"}, nil)
		if err == nil {
			t.Fatal("expected error for API error")
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("expected 429 in error, got: %v", err)
		}
	})

	t.Run("empty query returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"query": ""}, nil)
		if err == nil {
			t.Fatal("expected error for empty query")
		}
	})

	t.Run("uses config key when env var is empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Subscription-Token") != "config-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"web": map[string]any{
					"results": []map[string]any{
						{"title": "Result", "url": "https://example.com", "description": "desc"},
					},
				},
			})
		}))
		defer server.Close()

		origURL := braveSearchURL
		origClient := braveSearchHTTPClient
		origGetEnv := getEnvFunc
		braveSearchURL = server.URL
		braveSearchHTTPClient = server.Client()
		getEnvFunc = func(key string) string { return "" }
		defer func() {
			braveSearchURL = origURL
			braveSearchHTTPClient = origClient
			getEnvFunc = origGetEnv
		}()

		ctx := &ToolContext{BraveAPIKey: "config-key"}
		result, err := tool.Execute(map[string]any{"query": "test"}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Result") {
			t.Errorf("expected result, got: %s", result)
		}
	})
}

// ---------------------------------------------------------------------------
// web_fetch
// ---------------------------------------------------------------------------

func TestWebFetchTool(t *testing.T) {
	tool := webFetchTool()

	t.Run("fetches plain text", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("Hello, World!"))
		}))
		defer server.Close()

		origClient := webFetchHTTPClient
		webFetchHTTPClient = server.Client()
		defer func() { webFetchHTTPClient = origClient }()

		result, err := tool.Execute(map[string]any{"url": server.URL}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "Hello, World!" {
			t.Errorf("expected 'Hello, World!', got: %s", result)
		}
	})

	t.Run("strips HTML", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Test</title><style>body{}</style></head><body><h1>Hello</h1><p>World</p><script>alert('x')</script></body></html>`))
		}))
		defer server.Close()

		origClient := webFetchHTTPClient
		webFetchHTTPClient = server.Client()
		defer func() { webFetchHTTPClient = origClient }()

		result, err := tool.Execute(map[string]any{"url": server.URL}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Hello") {
			t.Errorf("expected 'Hello' in result, got: %s", result)
		}
		if !strings.Contains(result, "World") {
			t.Errorf("expected 'World' in result, got: %s", result)
		}
		if strings.Contains(result, "<h1>") {
			t.Errorf("expected no HTML tags, got: %s", result)
		}
		if strings.Contains(result, "alert") {
			t.Errorf("expected no script content, got: %s", result)
		}
		if strings.Contains(result, "body{}") {
			t.Errorf("expected no style content, got: %s", result)
		}
	})

	t.Run("HTTP error returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		origClient := webFetchHTTPClient
		webFetchHTTPClient = server.Client()
		defer func() { webFetchHTTPClient = origClient }()

		_, err := tool.Execute(map[string]any{"url": server.URL}, nil)
		if err == nil {
			t.Fatal("expected error for 404")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected 404 in error, got: %v", err)
		}
	})

	t.Run("empty URL returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"url": ""}, nil)
		if err == nil {
			t.Fatal("expected error for empty URL")
		}
	})

	t.Run("truncates large output", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(strings.Repeat("x", 60*1024)))
		}))
		defer server.Close()

		origClient := webFetchHTTPClient
		webFetchHTTPClient = server.Client()
		defer func() { webFetchHTTPClient = origClient }()

		result, err := tool.Execute(map[string]any{"url": server.URL}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "truncated at 50KB") {
			t.Errorf("expected truncation message, got length %d", len(result))
		}
	})
}

// ---------------------------------------------------------------------------
// htmlToText
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"zero maxLen", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// htmlToText
// ---------------------------------------------------------------------------

func TestHtmlToText(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		contains []string
		excludes []string
	}{
		{
			name:     "basic tags",
			html:     "<p>Hello</p><p>World</p>",
			contains: []string{"Hello", "World"},
		},
		{
			name:     "entities",
			html:     "&amp; &quot; &#39;",
			contains: []string{"&", "\"", "'"},
		},
		{
			name:     "strips script",
			html:     "before<script>var x=1;</script>after",
			contains: []string{"before", "after"},
			excludes: []string{"var x"},
		},
		{
			name:     "strips style",
			html:     "before<style>.x{color:red}</style>after",
			contains: []string{"before", "after"},
			excludes: []string{"color:red"},
		},
		{
			name:     "collapses whitespace",
			html:     "hello    world\n\n\tfoo",
			contains: []string{"hello world foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlToText(tt.html)
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("htmlToText(%q) = %q, expected to contain %q", tt.html, got, c)
				}
			}
			for _, e := range tt.excludes {
				if strings.Contains(got, e) {
					t.Errorf("htmlToText(%q) = %q, expected to not contain %q", tt.html, got, e)
				}
			}
		})
	}
}
