package provider

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIError represents a structured API error with retry metadata.
type APIError struct {
	StatusCode   int
	ErrorType    string
	Message      string
	RetryAfterMs int
}

// Error satisfies the error interface.
func (e *APIError) Error() string {
	if e.ErrorType != "" {
		return fmt.Sprintf("%s: %s", e.ErrorType, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// IsRetryable returns true for rate limit and overload errors.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case 429, 503, 529:
		return true
	}
	switch e.ErrorType {
	case "rate_limit_error", "overloaded_error":
		return true
	}
	// Mid-stream SSE errors (StatusCode 0) with retryable types.
	if e.StatusCode == 0 && e.ErrorType != "" {
		return e.ErrorType == "overloaded_error" || e.ErrorType == "api_error"
	}
	return false
}

// NewAPIError creates an APIError from HTTP response metadata.
func NewAPIError(statusCode int, errorType, message string, header http.Header) *APIError {
	return &APIError{
		StatusCode:   statusCode,
		ErrorType:    errorType,
		Message:      message,
		RetryAfterMs: parseRetryAfter(header),
	}
}

// parseRetryAfter extracts retry delay from HTTP headers.
// Checks Anthropic's retry-after-ms first, then standard Retry-After
// (seconds or HTTP-date format).
func parseRetryAfter(h http.Header) int {
	if h == nil {
		return 0
	}

	// Anthropic-specific: retry-after-ms (milliseconds)
	if ms := h.Get("retry-after-ms"); ms != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(ms)); err == nil && v > 0 {
			return v
		}
	}

	// Standard Retry-After header
	ra := h.Get("Retry-After")
	if ra == "" {
		return 0
	}
	ra = strings.TrimSpace(ra)

	// Try as seconds
	if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
		return secs * 1000
	}

	// Try as HTTP-date (RFC1123)
	if t, err := time.Parse(time.RFC1123, ra); err == nil {
		ms := int(time.Until(t).Milliseconds())
		if ms > 0 {
			return ms
		}
	}

	return 0
}
