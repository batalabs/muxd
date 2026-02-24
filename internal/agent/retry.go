package agent

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

const (
	maxRetries       = 5
	retryInitialWait = 2 * time.Second
	retryMaxWait     = 30 * time.Second
	retryMultiplier  = 2
)

// callProviderWithRetry wraps the provider API call with exponential backoff
// for retryable errors (rate limits, overloads).
func (a *Service) callProviderWithRetry(
	messages []domain.TranscriptMessage,
	toolSpecs []provider.ToolSpec,
	system string,
	onDelta func(string),
	onEvent EventFunc,
) ([]domain.ContentBlock, string, provider.Usage, error) {
	wait := retryInitialWait

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var blocks []domain.ContentBlock
		var stopReason string
		var usage provider.Usage
		var err error

		if a.prov == nil {
			return nil, "", provider.Usage{}, fmt.Errorf("no provider configured; use /config set model <provider>/<model>")
		}
		blocks, stopReason, usage, err = a.prov.StreamMessage(
			a.apiKey, a.modelID, messages, toolSpecs, system, onDelta,
		)

		if err == nil {
			return blocks, stopReason, usage, nil
		}

		if attempt >= maxRetries {
			return nil, "", provider.Usage{}, err
		}

		// Determine if error is retryable and what to wait
		retryWait := wait
		msg := ""
		var apiErr *provider.APIError
		if errors.As(err, &apiErr) && apiErr.IsRetryable() {
			// API error (429, 503, 529) — prefer server's Retry-After.
			// Don't cap the server's Retry-After; it knows when we can retry.
			if apiErr.RetryAfterMs > 0 {
				retryWait = time.Duration(apiErr.RetryAfterMs) * time.Millisecond
			} else if retryWait > retryMaxWait {
				retryWait = retryMaxWait
			}
			label := "Rate limited"
			if apiErr.StatusCode == 529 || apiErr.ErrorType == "overloaded_error" {
				label = "API overloaded"
			} else if apiErr.StatusCode == 503 {
				label = "Service unavailable"
			}
			msg = fmt.Sprintf("%s — retrying in %s (attempt %d/%d)", label, retryWait.Round(time.Millisecond), attempt+1, maxRetries)
		} else if isStreamError(err) {
			// Stream dropped mid-response (EOF, connection reset) — retry with backoff.
			// Flush stale pooled connections so the next attempt gets a fresh TCP/TLS
			// connection. Go's Transport only auto-retries stale connections for
			// idempotent methods (GET), not POST.
			provider.CloseIdleConnections()
			if retryWait > retryMaxWait {
				retryWait = retryMaxWait
			}
			msg = fmt.Sprintf("Connection lost — retrying in %s (attempt %d/%d)", retryWait.Round(time.Millisecond), attempt+1, maxRetries)
		} else {
			// Non-retryable error (auth, invalid request, etc.)
			return nil, "", provider.Usage{}, err
		}
		onEvent(Event{
			Kind:         EventRetrying,
			RetryAttempt: attempt + 1,
			RetryAfter:   retryWait,
			RetryMessage: msg,
		})

		if !a.sleepWithCancel(retryWait) {
			return nil, "", provider.Usage{}, fmt.Errorf("cancelled during retry wait")
		}

		// Exponential backoff for next attempt
		wait *= retryMultiplier
		if wait > retryMaxWait {
			wait = retryMaxWait
		}
	}

	return nil, "", provider.Usage{}, fmt.Errorf("max retries exceeded")
}

// isStreamError returns true for transient stream/connection errors that are
// worth retrying (e.g., connection dropped mid-response).
func isStreamError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected EOF") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "HTTP/1.x transport connection broken") ||
		strings.Contains(msg, "malformed chunked encoding") ||
		strings.Contains(msg, "chunked line ends with bare LF") ||
		strings.Contains(msg, "invalid byte in chunk length") ||
		strings.Contains(msg, "reading stream:")
}

// sleepWithCancel waits for the given duration, checking for cancellation
// every 100ms. Returns true if the sleep completed, false if cancelled.
func (a *Service) sleepWithCancel(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		cancelled := a.cancelled
		a.mu.Unlock()
		if cancelled {
			return false
		}
		remaining := time.Until(deadline)
		sleepStep := 100 * time.Millisecond
		if remaining < sleepStep {
			sleepStep = remaining
		}
		time.Sleep(sleepStep)
	}
	return true
}
