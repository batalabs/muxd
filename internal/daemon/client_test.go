package daemon

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDaemonClientHealth(t *testing.T) {
	t.Run("healthy server", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
		}))
		defer ts.Close()

		// Extract port from test server URL
		port := extractPort(t, ts.URL)
		client := NewDaemonClient(port)
		client.SetBaseURL(ts.URL)

		if err := client.Health(); err != nil {
			t.Fatalf("expected healthy, got error: %v", err)
		}
	})

	t.Run("unhealthy server", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		client := NewDaemonClient(0)
		client.SetBaseURL(ts.URL)

		if err := client.Health(); err == nil {
			t.Fatal("expected error for unhealthy server")
		}
	})
}

func TestDaemonClientCreateSession(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/sessions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"session_id":"abc-123"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	sessionID, err := client.CreateSession("/tmp/test", "model-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessionID != "abc-123" {
		t.Errorf("expected session_id abc-123, got %s", sessionID)
	}
}

func TestDaemonClientSubmitSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: delta\ndata: {\"text\":\"Hello\"}\n\n")
		fmt.Fprint(w, "event: stream_done\ndata: {\"input_tokens\":100,\"output_tokens\":50,\"stop_reason\":\"end_turn\"}\n\n")
		fmt.Fprint(w, "event: turn_done\ndata: {\"stop_reason\":\"end_turn\"}\n\n")
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	var events []SSEEvent
	err := client.Submit("test-session", "hello", func(evt SSEEvent) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != "delta" || events[0].DeltaText != "Hello" {
		t.Errorf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != "stream_done" || events[1].InputTokens != 100 {
		t.Errorf("unexpected second event: %+v", events[1])
	}
	if events[2].Type != "turn_done" {
		t.Errorf("unexpected third event: %+v", events[2])
	}
}

func TestDaemonClientSubmitSSEToolEvents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: tool_start\ndata: {\"tool_use_id\":\"t1\",\"tool_name\":\"file_read\"}\n\n")
		fmt.Fprint(w, "event: tool_done\ndata: {\"tool_use_id\":\"t1\",\"tool_name\":\"file_read\",\"result\":\"content\",\"is_error\":false}\n\n")
		fmt.Fprint(w, "event: ask_user\ndata: {\"ask_id\":\"a1\",\"prompt\":\"What language?\"}\n\n")
		fmt.Fprint(w, "event: compacted\ndata: {}\n\n")
		fmt.Fprint(w, "event: error\ndata: {\"error\":\"something failed\"}\n\n")
		fmt.Fprint(w, "event: turn_done\ndata: {\"stop_reason\":\"end_turn\"}\n\n")
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	var events []SSEEvent
	err := client.Submit("test-session", "hello", func(evt SSEEvent) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(events))
	}

	if events[0].Type != "tool_start" || events[0].ToolName != "file_read" {
		t.Errorf("unexpected tool_start: %+v", events[0])
	}
	if events[1].Type != "tool_done" || events[1].ToolResult != "content" {
		t.Errorf("unexpected tool_done: %+v", events[1])
	}
	if events[2].Type != "ask_user" || events[2].AskID != "a1" || events[2].AskPrompt != "What language?" {
		t.Errorf("unexpected ask_user: %+v", events[2])
	}
	if events[3].Type != "compacted" {
		t.Errorf("unexpected compacted: %+v", events[3])
	}
	if events[4].Type != "error" || events[4].ErrorMsg != "something failed" {
		t.Errorf("unexpected error: %+v", events[4])
	}
}

func TestDaemonClientAskResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/ask-response") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	err := client.SendAskResponse("session-1", "ask-1", "the answer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonClientWaitReady(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	if err := client.WaitReady(5 * time.Second); err != nil {
		t.Fatalf("expected ready, got error: %v", err)
	}
}

func TestParseSSEEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		data      string
		wantType  string
	}{
		{"delta", "delta", `{"text":"hi"}`, "delta"},
		{"tool_start", "tool_start", `{"tool_use_id":"t1","tool_name":"bash"}`, "tool_start"},
		{"tool_done", "tool_done", `{"tool_use_id":"t1","tool_name":"bash","result":"ok","is_error":false}`, "tool_done"},
		{"stream_done", "stream_done", `{"input_tokens":100,"output_tokens":50,"stop_reason":"end_turn"}`, "stream_done"},
		{"ask_user", "ask_user", `{"ask_id":"a1","prompt":"Question?"}`, "ask_user"},
		{"turn_done", "turn_done", `{"stop_reason":"end_turn"}`, "turn_done"},
		{"error", "error", `{"error":"bad stuff"}`, "error"},
		{"compacted", "compacted", `{}`, "compacted"},
		{"unknown", "unknown_event", `{}`, ""},
		{"invalid json", "delta", `not json`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := ParseSSEEvent(tt.eventType, tt.data)
			if evt.Type != tt.wantType {
				t.Errorf("ParseSSEEvent(%q, %q).Type = %q, want %q", tt.eventType, tt.data, evt.Type, tt.wantType)
			}
		})
	}
}

func TestParseSSEStream_toleratesUnexpectedEOFAfterCompletion(t *testing.T) {
	input := "" +
		"event: stream_done\ndata: {\"input_tokens\":1,\"output_tokens\":1,\"stop_reason\":\"end_turn\"}\n\n" +
		"event: turn_done\ndata: {\"stop_reason\":\"end_turn\"}\n\n"

	r := &unexpectedEOFReader{buf: bytes.NewBufferString(input)}
	var events []SSEEvent
	err := parseSSEStream(r, func(evt SSEEvent) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestParseSSEStream_unexpectedEOFBeforeCompletionReturnsError(t *testing.T) {
	input := "event: delta\ndata: {\"text\":\"partial\"}\n\n"
	r := &unexpectedEOFReader{buf: bytes.NewBufferString(input)}
	err := parseSSEStream(r, func(SSEEvent) {})
	if err == nil {
		t.Fatal("expected error")
	}
}

type unexpectedEOFReader struct {
	buf *bytes.Buffer
}

func (r *unexpectedEOFReader) Read(p []byte) (int, error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}
	return 0, io.ErrUnexpectedEOF
}

func TestDaemonClientCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/cancel") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	if err := client.Cancel("session-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonClientSetModel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/model") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	if err := client.SetModel("session-1", "sonnet", "claude-sonnet-4-6"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonClientGetSession(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"sess-1","title":"test"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	sess, err := client.GetSession("sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "sess-1" {
		t.Errorf("expected sess-1, got %q", sess.ID)
	}
}

func TestDaemonClientGetSession_notFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	_, err := client.GetSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestDaemonClientListSessions(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `[{"id":"s1"},{"id":"s2"}]`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	sessions, err := client.ListSessions("/tmp", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestDaemonClientGetMessages(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `[{"role":"user","content":"hello"}]`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	msgs, err := client.GetMessages("sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestDaemonClientGetConfig(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"footer_tokens":true,"footer_cost":true}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	prefs, err := client.GetConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prefs.FooterTokens {
		t.Error("expected FooterTokens=true")
	}
}

func TestDaemonClientSetConfig(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok","message":"Set model = gpt-4o"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	msg, err := client.SetConfig("model", "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "gpt-4o") {
		t.Errorf("expected message about gpt-4o, got %q", msg)
	}
}

func TestDaemonClientSetConfig_error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"error":"unknown key"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	_, err := client.SetConfig("bad.key", "value")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestDaemonClientGetMCPTools(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"tools":["tool1","tool2"],"statuses":{"srv1":"running"}}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	resp, err := client.GetMCPTools()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(resp.Tools))
	}
	if resp.Statuses["srv1"] != "running" {
		t.Errorf("expected srv1=running, got %q", resp.Statuses["srv1"])
	}
}

func TestDaemonClientGetMCPTools_httpError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	_, err := client.GetMCPTools()
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestDaemonClientBranchSession(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"new-sess","title":"branched"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	sess, err := client.BranchSession("orig-sess", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "new-sess" {
		t.Errorf("expected new-sess, got %q", sess.ID)
	}
}

func TestDaemonClientBranchSession_error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid sequence"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	_, err := client.BranchSession("sess-1", -1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDaemonClientSubmit_httpError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"empty text"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	err := client.Submit("sess-1", "hello", func(SSEEvent) {})
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestDaemonClientCreateSession_error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"error":"db failure"}`)
	}))
	defer ts.Close()

	client := NewDaemonClient(0)
	client.SetBaseURL(ts.URL)

	_, err := client.CreateSession("/tmp", "model")
	if err == nil {
		t.Fatal("expected error when server returns error field")
	}
}

func TestDaemonClientWaitReady_timeout(t *testing.T) {
	// Connect to a port where nothing is listening
	client := NewDaemonClient(0)
	client.SetBaseURL("http://localhost:19999")

	err := client.WaitReady(100 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestParseSSEEvent_titled(t *testing.T) {
	evt := ParseSSEEvent("titled", `{"title":"My Session","tags":"go,test"}`)
	if evt.Type != "titled" {
		t.Errorf("expected titled, got %q", evt.Type)
	}
	if evt.Title != "My Session" {
		t.Errorf("expected title 'My Session', got %q", evt.Title)
	}
	if evt.Tags != "go,test" {
		t.Errorf("expected tags 'go,test', got %q", evt.Tags)
	}
}

func TestParseSSEEvent_retrying(t *testing.T) {
	evt := ParseSSEEvent("retrying", `{"attempt":2,"wait_ms":5000,"message":"rate limited"}`)
	if evt.Type != "retrying" {
		t.Errorf("expected retrying, got %q", evt.Type)
	}
	if evt.RetryAttempt != 2 {
		t.Errorf("expected attempt 2, got %d", evt.RetryAttempt)
	}
	if evt.RetryWaitMs != 5000 {
		t.Errorf("expected wait_ms 5000, got %d", evt.RetryWaitMs)
	}
	if evt.RetryMessage != "rate limited" {
		t.Errorf("expected message 'rate limited', got %q", evt.RetryMessage)
	}
}

func TestIsRecoverableSSEStreamErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"string unexpected EOF", fmt.Errorf("something: unexpected EOF"), true},
		{"bare LF", fmt.Errorf("chunked line ends with bare LF"), true},
		{"invalid chunk", fmt.Errorf("invalid byte in chunk length"), true},
		{"regular error", fmt.Errorf("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRecoverableSSEStreamErr(tt.err)
			if got != tt.want {
				t.Errorf("isRecoverableSSEStreamErr(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewDaemonClient(t *testing.T) {
	client := NewDaemonClient(8080)
	if client.baseURL != "http://localhost:8080" {
		t.Errorf("expected base URL http://localhost:8080, got %q", client.baseURL)
	}
	if client.httpClient == nil {
		t.Error("expected non-nil http client")
	}
}

func TestSetAuthToken(t *testing.T) {
	client := NewDaemonClient(0)
	client.SetAuthToken("  my-token  ")
	if client.authToken != "my-token" {
		t.Errorf("expected trimmed token, got %q", client.authToken)
	}
}

func TestSetBaseURL(t *testing.T) {
	client := NewDaemonClient(0)
	client.SetBaseURL("http://custom:9999")
	if client.baseURL != "http://custom:9999" {
		t.Errorf("expected custom URL, got %q", client.baseURL)
	}
}

func TestParseSSEEvent_streamDone_allFields(t *testing.T) {
	evt := ParseSSEEvent("stream_done", `{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":20,"stop_reason":"end_turn"}`)
	if evt.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", evt.InputTokens)
	}
	if evt.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", evt.OutputTokens)
	}
	if evt.CacheCreationInputTokens != 10 {
		t.Errorf("CacheCreationInputTokens = %d, want 10", evt.CacheCreationInputTokens)
	}
	if evt.CacheReadInputTokens != 20 {
		t.Errorf("CacheReadInputTokens = %d, want 20", evt.CacheReadInputTokens)
	}
	if evt.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", evt.StopReason)
	}
}

// extractPort extracts the port number from a URL like "http://127.0.0.1:12345"
func extractPort(t *testing.T, url string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(url, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("extracting port from %q: %v", url, err)
	}
	return port
}
