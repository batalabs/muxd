package daemon

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/agent"
	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"

	_ "modernc.org/sqlite"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	st, err := store.NewFromDB(db)
	if err != nil {
		t.Fatal(err)
	}

	prefs := config.DefaultPreferences()
	srv := NewServer(st, "test-key", "test-model", "test-label", nil, &prefs)
	return srv, st
}

func newAuthedRequest(srv *Server, method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	return req
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
}

func TestCreateSession(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body, _ := json.Marshal(map[string]string{
		"project_path": "/tmp/test",
		"model_id":     "test-model",
	})
	req := newAuthedRequest(srv, "POST", "/api/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["session_id"] == "" {
		t.Error("expected non-empty session_id")
	}
}

func TestListSessions(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Create two sessions
	_, _ = st.CreateSession("/tmp/test", "model-a")
	_, _ = st.CreateSession("/tmp/test", "model-b")

	req := newAuthedRequest(srv, "GET", "/api/sessions?project=/tmp/test&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sessions []domain.Session
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestGetSession(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	req := newAuthedRequest(srv, "GET", "/api/sessions/"+sess.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetSessionNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := newAuthedRequest(srv, "GET", "/api/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetMessages(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")
	_ = st.AppendMessage(sess.ID, "user", "hello", 0)
	_ = st.AppendMessage(sess.ID, "assistant", "hi there", 10)

	req := newAuthedRequest(srv, "GET", "/api/sessions/"+sess.ID+"/messages", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var msgs []domain.TranscriptMessage
	if err := json.NewDecoder(w.Body).Decode(&msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestSetModel(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	body, _ := json.Marshal(map[string]string{
		"label":    "gpt-4o",
		"model_id": "gpt-4o",
	})
	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/model", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetModelSwitchesProviderFromOllamaToAnthropic(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Start as ollama provider.
	ollamaProv, err := provider.GetProvider("ollama")
	if err != nil {
		t.Fatalf("getting ollama provider: %v", err)
	}
	srv.provider = ollamaProv
	srv.modelID = "gemma3:4b"
	srv.modelLabel = "gemma3:4b"
	srv.prefs.AnthropicAPIKey = "test-anthropic-key"

	sess, _ := st.CreateSession("/tmp/test", "gemma3:4b")

	body, _ := json.Marshal(map[string]string{
		"label":    "claude-sonnet-4-6",
		"model_id": "claude-sonnet-4-6",
	})
	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/model", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if srv.provider == nil || srv.provider.Name() != "anthropic" {
		t.Fatalf("expected provider anthropic, got %#v", srv.provider)
	}
	if srv.apiKey != "test-anthropic-key" {
		t.Fatalf("expected anthropic api key to be loaded, got %q", srv.apiKey)
	}
}

func TestConfigEndpoints(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	t.Run("get config", func(t *testing.T) {
		req := newAuthedRequest(srv, "GET", "/api/config", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var prefs config.Preferences
		if err := json.NewDecoder(w.Body).Decode(&prefs); err != nil {
			t.Fatal(err)
		}
		if !prefs.FooterTokens {
			t.Error("expected default FooterTokens to be true")
		}
	})

	t.Run("set config invalid key", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"key":   "invalid.key",
			"value": "value",
		})
		req := newAuthedRequest(srv, "POST", "/api/config", bytes.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestCancelNoAgent(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for no agent, got %d", w.Code)
	}
}

func TestSubmitEmptyText(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	body, _ := json.Marshal(map[string]string{"text": ""})
	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/submit", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAskResponseUnknownID(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	body, _ := json.Marshal(map[string]string{
		"ask_id": "nonexistent",
		"answer": "test",
	})
	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/ask-response", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestWriteSSE(t *testing.T) {
	w := httptest.NewRecorder()
	// httptest.ResponseRecorder implements http.Flusher
	var flusher http.Flusher = w

	writeSSE(w, flusher, "delta", map[string]string{"text": "hello"})

	body := w.Body.String()
	if !strings.Contains(body, "event: delta") {
		t.Error("expected SSE event header")
	}
	if !strings.Contains(body, `"text":"hello"`) {
		t.Error("expected SSE data")
	}
}

func TestAuthMiddleware(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	t.Run("rejects missing auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("rejects wrong token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("accepts valid Bearer token", func(t *testing.T) {
		req := newAuthedRequest(srv, "GET", "/api/sessions", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("accepts raw token without Bearer prefix", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		req.Header.Set("Authorization", srv.AuthToken())
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestExtractTweetID(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		{"valid format", "Posted tweet 12345\nMore info", "12345"},
		{"case insensitive", "posted Tweet 67890", "67890"},
		{"empty input", "", ""},
		{"no match", "Something else entirely", ""},
		{"only one word", "hello", ""},
		{"two words no match", "hello world", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTweetID(tt.input)
			if got != tt.wantID {
				t.Errorf("extractTweetID(%q) = %q, want %q", tt.input, got, tt.wantID)
			}
		})
	}
}

func TestGenerateAuthToken(t *testing.T) {
	token := generateAuthToken()
	if len(token) != 64 { // 32 bytes * 2 hex chars
		t.Errorf("expected 64-char hex token, got length %d", len(token))
	}

	// Should be different each time
	token2 := generateAuthToken()
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestNewServer(t *testing.T) {
	prefs := config.DefaultPreferences()
	srv := NewServer(nil, "key", "model", "label", nil, &prefs)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.AuthToken() == "" {
		t.Error("expected non-empty auth token")
	}
	if srv.agents == nil {
		t.Error("expected initialized agents map")
	}
	if srv.askChans == nil {
		t.Error("expected initialized askChans map")
	}
}

func TestSetAgentFactory(t *testing.T) {
	srv, _ := newTestServer(t)
	if srv.newAgent != nil {
		t.Error("expected nil newAgent initially")
	}
	srv.SetAgentFactory(func(apiKey, modelID, modelLabel string, st *store.Store, sess *domain.Session, prov provider.Provider) *agent.Service {
		return nil
	})
	if srv.newAgent == nil {
		t.Error("expected newAgent to be set")
	}
}

func TestSetDetectGitRepo(t *testing.T) {
	srv, _ := newTestServer(t)
	if srv.detectGitRepo != nil {
		t.Error("expected nil detectGitRepo initially")
	}
	srv.SetDetectGitRepo(func() (string, bool) { return "/repo", true })
	if srv.detectGitRepo == nil {
		t.Error("expected detectGitRepo to be set")
	}
}

func TestSetQuiet(t *testing.T) {
	srv, _ := newTestServer(t)
	if srv.quiet {
		t.Error("expected quiet=false initially")
	}
	srv.SetQuiet(true)
	if !srv.quiet {
		t.Error("expected quiet=true after SetQuiet")
	}
}

func TestHandleBranch(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")
	// Add a message so branching has something
	_ = st.AppendMessage(sess.ID, "user", "hello", 0)
	_ = st.AppendMessage(sess.ID, "assistant", "hi", 10)

	body, _ := json.Marshal(map[string]int{"at_sequence": 1})
	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/branch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var newSess domain.Session
	if err := json.NewDecoder(w.Body).Decode(&newSess); err != nil {
		t.Fatal(err)
	}
	if newSess.ID == "" || newSess.ID == sess.ID {
		t.Error("expected a new session ID from branch")
	}
}

func TestHandleBranch_invalidBody(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/branch", strings.NewReader("bad json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSetConfig_validKey(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body, _ := json.Marshal(map[string]string{
		"key":   "footer.tokens",
		"value": "false",
	})
	req := newAuthedRequest(srv, "POST", "/api/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %q", resp["status"])
	}
}

func TestHandleSetConfig_invalidBody(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := newAuthedRequest(srv, "POST", "/api/config", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCreateSession_invalidBody(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := newAuthedRequest(srv, "POST", "/api/sessions", strings.NewReader("bad json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmit_invalidBody(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/submit", strings.NewReader("bad json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmit_noAgentFactory(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")
	// No agent factory set → getOrCreateAgent should fail

	body, _ := json.Marshal(map[string]string{"text": "hello"})
	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/submit", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAskResponse_invalidBody(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/ask-response", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSetModel_invalidBody(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/model", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleMCPTools_noManager(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	req := newAuthedRequest(srv, "GET", "/api/mcp/tools", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	tools, ok := resp["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array")
	}
	if len(tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(tools))
	}
}

func TestHandleListSessions_withLimit(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	for i := 0; i < 5; i++ {
		_, _ = st.CreateSession("/tmp/test", "model-a")
	}

	req := newAuthedRequest(srv, "GET", "/api/sessions?project=/tmp/test&limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sessions []domain.Session
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions with limit=2, got %d", len(sessions))
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, map[string]string{"key": "value"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"key":"value"`) {
		t.Errorf("expected JSON body, got %q", w.Body.String())
	}
}
