package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// http_request — generic REST API caller
// ---------------------------------------------------------------------------

func httpRequestTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "http_request",
			Description: "Make an HTTP request to any URL. Supports GET, POST, PUT, PATCH, DELETE. Use for calling REST APIs, webhooks, or any HTTP endpoint. Returns status code, selected headers, and response body. Confirm with the user before making requests that modify external state (POST, PUT, DELETE).",
			Properties: map[string]provider.ToolProp{
				"method":  {Type: "string", Description: "HTTP method: GET, POST, PUT, PATCH, DELETE (default: GET)"},
				"url":     {Type: "string", Description: "Full URL to request (e.g. 'https://api.example.com/data')"},
				"headers": {Type: "object", Description: "Request headers as key-value pairs (e.g. {\"Authorization\": \"Bearer token\", \"Content-Type\": \"application/json\"})"},
				"body":    {Type: "string", Description: "Request body (typically JSON for POST/PUT/PATCH)"},
				"timeout": {Type: "integer", Description: "Timeout in seconds (default: 30, max: 120)"},
			},
			Required: []string{"url"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			rawURL, ok := input["url"].(string)
			if !ok || rawURL == "" {
				return "", fmt.Errorf("url is required")
			}

			method := "GET"
			if v, ok := input["method"].(string); ok && v != "" {
				method = strings.ToUpper(v)
			}

			switch method {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
				// valid
			default:
				return "", fmt.Errorf("unsupported HTTP method: %s", method)
			}

			timeout := 30
			if v, ok := input["timeout"].(float64); ok && v > 0 {
				timeout = int(v)
				if timeout > 120 {
					timeout = 120
				}
			}

			var bodyReader io.Reader
			if v, ok := input["body"].(string); ok && v != "" {
				bodyReader = bytes.NewBufferString(v)
			}

			req, err := http.NewRequest(method, rawURL, bodyReader)
			if err != nil {
				return "", fmt.Errorf("creating request: %w", err)
			}

			req.Header.Set("User-Agent", "muxd/1.0")

			// Apply custom headers.
			if hdrs, ok := input["headers"].(map[string]any); ok {
				for k, v := range hdrs {
					if sv, ok := v.(string); ok {
						req.Header.Set(k, sv)
					}
				}
			}

			client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			// Read body up to 100KB.
			const maxBody = 100 * 1024
			limited := io.LimitReader(resp.Body, maxBody+1)
			body, err := io.ReadAll(limited)
			if err != nil {
				return "", fmt.Errorf("reading response: %w", err)
			}

			truncated := false
			if len(body) > maxBody {
				body = body[:maxBody]
				truncated = true
			}

			// Format response.
			var b strings.Builder
			fmt.Fprintf(&b, "HTTP %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))

			// Include useful response headers.
			for _, h := range []string{"Content-Type", "Location", "X-Request-Id", "X-RateLimit-Remaining", "Retry-After"} {
				if v := resp.Header.Get(h); v != "" {
					fmt.Fprintf(&b, "%s: %s\n", h, v)
				}
			}
			b.WriteString("\n")

			bodyStr := string(body)

			// Try to pretty-print JSON.
			if strings.Contains(resp.Header.Get("Content-Type"), "json") {
				var pretty bytes.Buffer
				if json.Indent(&pretty, body, "", "  ") == nil {
					bodyStr = pretty.String()
				}
			}

			b.WriteString(bodyStr)
			if truncated {
				b.WriteString("\n... (truncated at 100KB)")
			}

			return b.String(), nil
		},
	}
}
