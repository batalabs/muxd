package provider

import (
	"net/http"
	"testing"
	"time"
)

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      APIError
		expected string
	}{
		{
			name:     "with error type",
			err:      APIError{StatusCode: 429, ErrorType: "rate_limit_error", Message: "too many requests"},
			expected: "rate_limit_error: too many requests",
		},
		{
			name:     "without error type",
			err:      APIError{StatusCode: 500, Message: "internal server error"},
			expected: "HTTP 500: internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       APIError
		retryable bool
	}{
		{
			name:      "429 status",
			err:       APIError{StatusCode: 429, ErrorType: "rate_limit_error"},
			retryable: true,
		},
		{
			name:      "503 status",
			err:       APIError{StatusCode: 503, ErrorType: "api_error"},
			retryable: true,
		},
		{
			name:      "529 status",
			err:       APIError{StatusCode: 529, ErrorType: "overloaded_error"},
			retryable: true,
		},
		{
			name:      "rate_limit_error type with non-429 code",
			err:       APIError{StatusCode: 400, ErrorType: "rate_limit_error"},
			retryable: true,
		},
		{
			name:      "overloaded_error type",
			err:       APIError{StatusCode: 500, ErrorType: "overloaded_error"},
			retryable: true,
		},
		{
			name:      "400 invalid_request_error",
			err:       APIError{StatusCode: 400, ErrorType: "invalid_request_error"},
			retryable: false,
		},
		{
			name:      "401 authentication_error",
			err:       APIError{StatusCode: 401, ErrorType: "authentication_error"},
			retryable: false,
		},
		{
			name:      "500 generic",
			err:       APIError{StatusCode: 500, ErrorType: "api_error"},
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.IsRetryable()
			if got != tt.retryable {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.retryable)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		wantMs  int
	}{
		{
			name:    "nil headers",
			headers: nil,
			wantMs:  0,
		},
		{
			name:    "empty headers",
			headers: http.Header{},
			wantMs:  0,
		},
		{
			name: "retry-after-ms Anthropic header",
			headers: http.Header{
				"Retry-After-Ms": []string{"500"},
			},
			wantMs: 500,
		},
		{
			name: "Retry-After seconds",
			headers: http.Header{
				"Retry-After": []string{"3"},
			},
			wantMs: 3000,
		},
		{
			name: "retry-after-ms takes priority over Retry-After",
			headers: http.Header{
				"Retry-After-Ms": []string{"200"},
				"Retry-After":    []string{"5"},
			},
			wantMs: 200,
		},
		{
			name: "Retry-After HTTP-date",
			headers: http.Header{
				"Retry-After": []string{time.Now().Add(2 * time.Second).UTC().Format(time.RFC1123)},
			},
			wantMs: 2000,
		},
		{
			name: "invalid retry-after-ms",
			headers: http.Header{
				"Retry-After-Ms": []string{"not-a-number"},
			},
			wantMs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := tt.headers
			wantMs := tt.wantMs
			// Compute HTTP-date header fresh to avoid timing skew.
			if tt.name == "Retry-After HTTP-date" {
				headers = http.Header{
					"Retry-After": []string{time.Now().Add(2 * time.Second).UTC().Format(time.RFC1123)},
				}
				wantMs = 2000
			}
			got := parseRetryAfter(headers)
			// Allow 1000ms tolerance for HTTP-date tests (CI can be slow).
			if tt.name == "Retry-After HTTP-date" {
				if got < wantMs-1000 || got > wantMs+1000 {
					t.Errorf("parseRetryAfter() = %d, want ~%d (±1000ms)", got, wantMs)
				}
			} else {
				if got != wantMs {
					t.Errorf("parseRetryAfter() = %d, want %d", got, wantMs)
				}
			}
		})
	}
}
