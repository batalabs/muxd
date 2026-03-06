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
	err := client.Submit("test-session", "hello", nil, func(evt SSEEvent) {
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
	err := client.Submit("test-session", "hello", nil, func(evt SSEEvent) {
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

func TestParseSSEEvent_delta(t *testing.T) {
	evt := ParseSSEEvent("delta", `{"text":"Hello, world!"}`)
	if evt.Type != "delta" {
		t.Errorf("Type = %q, want %q", evt.Type, "delta")
	}
	if evt.DeltaText != "Hello, world!" {
		t.Errorf("DeltaText = %q, want %q", evt.DeltaText, "Hello, world!")
	}
}

func TestParseSSEEvent_tool_start(t *testing.T) {
	evt := ParseSSEEvent("tool_start", `{"tool_use_id":"tu_abc123","tool_name":"file_read"}`)
	if evt.Type != "tool_start" {
		t.Errorf("Type = %q, want %q", evt.Type, "tool_start")
	}
	if evt.ToolUseID != "tu_abc123" {
		t.Errorf("ToolUseID = %q, want %q", evt.ToolUseID, "tu_abc123")
	}
	if evt.ToolName != "file_read" {
		t.Errorf("ToolName = %q, want %q", evt.ToolName, "file_read")
	}
}

func TestParseSSEEvent_tool_done(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		wantResult  string
		wantIsError bool
	}{
		{
			name:        "success result",
			data:        `{"tool_use_id":"t1","tool_name":"bash","result":"file contents here","is_error":false}`,
			wantResult:  "file contents here",
			wantIsError: false,
		},
		{
			name:        "error result",
			data:        `{"tool_use_id":"t1","tool_name":"bash","result":"command failed","is_error":true}`,
			wantResult:  "command failed",
			wantIsError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := ParseSSEEvent("tool_done", tt.data)
			if evt.Type != "tool_done" {
				t.Errorf("Type = %q, want %q", evt.Type, "tool_done")
			}
			if evt.ToolResult != tt.wantResult {
				t.Errorf("ToolResult = %q, want %q", evt.ToolResult, tt.wantResult)
			}
			if evt.ToolIsError != tt.wantIsError {
				t.Errorf("ToolIsError = %v, want %v", evt.ToolIsError, tt.wantIsError)
			}
		})
	}
}

func TestParseSSEEvent_stream_done(t *testing.T) {
	evt := ParseSSEEvent("stream_done", `{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":30,"cache_read_input_tokens":150,"stop_reason":"end_turn"}`)
	if evt.Type != "stream_done" {
		t.Errorf("Type = %q, want %q", evt.Type, "stream_done")
	}
	if evt.InputTokens != 500 {
		t.Errorf("InputTokens = %d, want 500", evt.InputTokens)
	}
	if evt.OutputTokens != 200 {
		t.Errorf("OutputTokens = %d, want 200", evt.OutputTokens)
	}
	if evt.CacheCreationInputTokens != 30 {
		t.Errorf("CacheCreationInputTokens = %d, want 30", evt.CacheCreationInputTokens)
	}
	if evt.CacheReadInputTokens != 150 {
		t.Errorf("CacheReadInputTokens = %d, want 150", evt.CacheReadInputTokens)
	}
	if evt.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", evt.StopReason, "end_turn")
	}
}

func TestParseSSEEvent_ask_user(t *testing.T) {
	evt := ParseSSEEvent("ask_user", `{"ask_id":"ask_xyz","prompt":"Allow file write to main.go?"}`)
	if evt.Type != "ask_user" {
		t.Errorf("Type = %q, want %q", evt.Type, "ask_user")
	}
	if evt.AskID != "ask_xyz" {
		t.Errorf("AskID = %q, want %q", evt.AskID, "ask_xyz")
	}
	if evt.AskPrompt != "Allow file write to main.go?" {
		t.Errorf("AskPrompt = %q, want %q", evt.AskPrompt, "Allow file write to main.go?")
	}
}

func TestParseSSEEvent_error(t *testing.T) {
	evt := ParseSSEEvent("error", `{"error":"rate limit exceeded"}`)
	if evt.Type != "error" {
		t.Errorf("Type = %q, want %q", evt.Type, "error")
	}
	if evt.ErrorMsg != "rate limit exceeded" {
		t.Errorf("ErrorMsg = %q, want %q", evt.ErrorMsg, "rate limit exceeded")
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

	err := client.Submit("sess-1", "hello", nil, func(SSEEvent) {})
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
	evt := ParseSSEEvent("titled", `{"title":"My Session","tags":"go,test","model":"claude-sonnet-4-20250514"}`)
	if evt.Type != "titled" {
		t.Errorf("Type = %q, want %q", evt.Type, "titled")
	}
	if evt.Title != "My Session" {
		t.Errorf("Title = %q, want %q", evt.Title, "My Session")
	}
	if evt.Tags != "go,test" {
		t.Errorf("Tags = %q, want %q", evt.Tags, "go,test")
	}
	if evt.ModelUsed != "claude-sonnet-4-20250514" {
		t.Errorf("ModelUsed = %q, want %q", evt.ModelUsed, "claude-sonnet-4-20250514")
	}
}

func TestParseSSEEvent_retrying(t *testing.T) {
	evt := ParseSSEEvent("retrying", `{"attempt":3,"wait_ms":10000,"message":"overloaded, backing off"}`)
	if evt.Type != "retrying" {
		t.Errorf("Type = %q, want %q", evt.Type, "retrying")
	}
	if evt.RetryAttempt != 3 {
		t.Errorf("RetryAttempt = %d, want 3", evt.RetryAttempt)
	}
	if evt.RetryWaitMs != 10000 {
		t.Errorf("RetryWaitMs = %d, want 10000", evt.RetryWaitMs)
	}
	if evt.RetryMessage != "overloaded, backing off" {
		t.Errorf("RetryMessage = %q, want %q", evt.RetryMessage, "overloaded, backing off")
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

func TestParseSSEEvent_unknown_event(t *testing.T) {
	evt := ParseSSEEvent("something_new", `{"foo":"bar"}`)
	if evt != (SSEEvent{}) {
		t.Errorf("expected zero SSEEvent for unknown type, got %+v", evt)
	}
}

func TestParseSSEEvent_invalid_json(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"not json at all", `not json`},
		{"truncated json", `{"text": "hel`},
		{"empty string", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := ParseSSEEvent("delta", tt.data)
			if evt != (SSEEvent{}) {
				t.Errorf("expected zero SSEEvent for invalid JSON %q, got %+v", tt.data, evt)
			}
		})
	}
}

func TestParseSSEEvent_missing_fields(t *testing.T) {
	// Valid JSON but none of the expected fields present — all should be zero values.
	tests := []struct {
		name      string
		eventType string
	}{
		{"delta with empty object", "delta"},
		{"tool_start with empty object", "tool_start"},
		{"tool_done with empty object", "tool_done"},
		{"stream_done with empty object", "stream_done"},
		{"ask_user with empty object", "ask_user"},
		{"error with empty object", "error"},
		{"titled with empty object", "titled"},
		{"retrying with empty object", "retrying"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := ParseSSEEvent(tt.eventType, `{}`)
			if evt.Type != tt.eventType {
				t.Errorf("Type = %q, want %q", evt.Type, tt.eventType)
			}
			// All fields except Type should be zero values.
			if evt.DeltaText != "" {
				t.Errorf("DeltaText = %q, want empty", evt.DeltaText)
			}
			if evt.ToolUseID != "" {
				t.Errorf("ToolUseID = %q, want empty", evt.ToolUseID)
			}
			if evt.ToolName != "" {
				t.Errorf("ToolName = %q, want empty", evt.ToolName)
			}
			if evt.ToolResult != "" {
				t.Errorf("ToolResult = %q, want empty", evt.ToolResult)
			}
			if evt.ToolIsError != false {
				t.Errorf("ToolIsError = %v, want false", evt.ToolIsError)
			}
			if evt.InputTokens != 0 {
				t.Errorf("InputTokens = %d, want 0", evt.InputTokens)
			}
			if evt.OutputTokens != 0 {
				t.Errorf("OutputTokens = %d, want 0", evt.OutputTokens)
			}
			if evt.CacheCreationInputTokens != 0 {
				t.Errorf("CacheCreationInputTokens = %d, want 0", evt.CacheCreationInputTokens)
			}
			if evt.CacheReadInputTokens != 0 {
				t.Errorf("CacheReadInputTokens = %d, want 0", evt.CacheReadInputTokens)
			}
			if evt.StopReason != "" {
				t.Errorf("StopReason = %q, want empty", evt.StopReason)
			}
			if evt.AskID != "" {
				t.Errorf("AskID = %q, want empty", evt.AskID)
			}
			if evt.AskPrompt != "" {
				t.Errorf("AskPrompt = %q, want empty", evt.AskPrompt)
			}
			if evt.ErrorMsg != "" {
				t.Errorf("ErrorMsg = %q, want empty", evt.ErrorMsg)
			}
			if evt.Title != "" {
				t.Errorf("Title = %q, want empty", evt.Title)
			}
			if evt.Tags != "" {
				t.Errorf("Tags = %q, want empty", evt.Tags)
			}
			if evt.ModelUsed != "" {
				t.Errorf("ModelUsed = %q, want empty", evt.ModelUsed)
			}
			if evt.RetryAttempt != 0 {
				t.Errorf("RetryAttempt = %d, want 0", evt.RetryAttempt)
			}
			if evt.RetryWaitMs != 0 {
				t.Errorf("RetryWaitMs = %d, want 0", evt.RetryWaitMs)
			}
			if evt.RetryMessage != "" {
				t.Errorf("RetryMessage = %q, want empty", evt.RetryMessage)
			}
		})
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
