package agent

import (
	"errors"
	"io"
	"testing"
)

func TestIsStreamError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"transport broken", errors.New("HTTP/1.x transport connection broken"), true},
		{"malformed chunked", errors.New("malformed chunked encoding"), true},
		{"bare LF", errors.New("chunked line ends with bare LF"), true},
		{"invalid chunk", errors.New("invalid byte in chunk length"), true},
		{"reading stream", errors.New("reading stream: connection dropped"), true},
		{"response body closed", errors.New("http2: response body closed"), true},
		{"stream error", errors.New("stream error: stream ID 1; INTERNAL_ERROR"), true},
		{"closed connection", errors.New("use of closed network connection"), true},
		{"auth error", errors.New("401 unauthorized"), false},
		{"generic error", errors.New("something went wrong"), false},
		{"wrapped unexpected EOF", errors.New("stream: unexpected EOF"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStreamError(tt.err); got != tt.want {
				t.Errorf("isStreamError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSleepWithCancel(t *testing.T) {
	t.Run("completes when not canceled", func(t *testing.T) {
		svc := &Service{}
		// Very short sleep
		if !svc.sleepWithCancel(1) {
			t.Error("expected sleepWithCancel to return true (completed)")
		}
	})

	t.Run("returns false when canceled", func(t *testing.T) {
		svc := &Service{canceled: true}
		if svc.sleepWithCancel(1_000_000_000) {
			t.Error("expected sleepWithCancel to return false (canceled)")
		}
	})
}
