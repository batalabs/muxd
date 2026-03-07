package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/batalabs/muxd/internal/agent"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"
)

// stubAgentFactory returns an AgentFactory that creates real agent.Service
// instances with a nil provider (sufficient for testing server wiring).
func stubAgentFactory() AgentFactory {
	return func(apiKey, modelID, modelLabel string, st *store.Store, sess *domain.Session, prov provider.Provider) *agent.Service {
		return agent.NewService(apiKey, modelID, modelLabel, st, sess, prov)
	}
}

func TestServer_ConfigSetPropagatesAPIKey(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Set the active provider to anthropic so that setting anthropic.api_key
	// propagates to srv.apiKey.
	anthropicProv, err := provider.GetProvider("anthropic")
	if err != nil {
		t.Fatalf("getting anthropic provider: %v", err)
	}
	srv.provider = anthropicProv

	// Set a new anthropic API key via POST /api/config.
	body, _ := json.Marshal(map[string]string{
		"key":   "anthropic.api_key",
		"value": "sk-ant-new-test-key-12345",
	})
	req := newAuthedRequest(srv, "POST", "/api/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the server's active API key was updated.
	if srv.apiKey != "sk-ant-new-test-key-12345" {
		t.Errorf("expected server apiKey to be updated, got %q", srv.apiKey)
	}

	// Verify the preference was persisted in memory.
	if srv.prefs.AnthropicAPIKey != "sk-ant-new-test-key-12345" {
		t.Errorf("expected prefs.AnthropicAPIKey to be set, got %q", srv.prefs.AnthropicAPIKey)
	}

	// Now set up an agent factory and create a session to verify the agent
	// receives the updated key.
	srv.SetAgentFactory(stubAgentFactory())
	sess, err := st.CreateSession("/tmp/test", "test-model")
	if err != nil {
		t.Fatal(err)
	}

	ag, err := srv.getOrCreateAgent(sess.ID)
	if err != nil {
		t.Fatalf("getOrCreateAgent failed: %v", err)
	}
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestServer_ModelSwitchUpdatesProvider(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Start with ollama provider.
	ollamaProv, err := provider.GetProvider("ollama")
	if err != nil {
		t.Fatalf("getting ollama provider: %v", err)
	}
	srv.provider = ollamaProv
	srv.modelID = "gemma3:4b"
	srv.modelLabel = "gemma3:4b"
	srv.prefs.AnthropicAPIKey = "test-anthropic-key-for-switch"

	sess, _ := st.CreateSession("/tmp/test", "gemma3:4b")

	t.Run("switch from ollama to anthropic model", func(t *testing.T) {
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

		// Provider should now be anthropic.
		if srv.provider == nil {
			t.Fatal("expected non-nil provider")
		}
		if srv.provider.Name() != "anthropic" {
			t.Errorf("expected provider 'anthropic', got %q", srv.provider.Name())
		}

		// Model ID and label should be updated.
		if srv.modelID != "claude-sonnet-4-6" {
			t.Errorf("expected modelID 'claude-sonnet-4-6', got %q", srv.modelID)
		}
		if srv.modelLabel != "claude-sonnet-4-6" {
			t.Errorf("expected modelLabel 'claude-sonnet-4-6', got %q", srv.modelLabel)
		}

		// API key should be loaded from prefs for the new provider.
		if srv.apiKey != "test-anthropic-key-for-switch" {
			t.Errorf("expected apiKey to be loaded for anthropic, got %q", srv.apiKey)
		}
	})

	t.Run("switch to ollama model", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"label":    "llama3:8b",
			"model_id": "llama3:8b",
		})
		req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/model", bytes.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		if srv.provider.Name() != "ollama" {
			t.Errorf("expected provider 'ollama', got %q", srv.provider.Name())
		}
		if srv.modelID != "llama3:8b" {
			t.Errorf("expected modelID 'llama3:8b', got %q", srv.modelID)
		}
	})
}

func TestServer_SessionNotFoundReturns404(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	srv.SetAgentFactory(stubAgentFactory())

	t.Run("submit to nonexistent session", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"text": "hello"})
		req := newAuthedRequest(srv, "POST", "/api/sessions/nonexistent-session-id/submit", bytes.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for nonexistent session, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("get nonexistent session", func(t *testing.T) {
		req := newAuthedRequest(srv, "GET", "/api/sessions/nonexistent-session-id", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("cancel nonexistent session", func(t *testing.T) {
		req := newAuthedRequest(srv, "POST", "/api/sessions/nonexistent-session-id/cancel", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestServer_CreateAndResumeSession(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	srv.SetAgentFactory(stubAgentFactory())

	// Create a session via the API.
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

	var createResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatal(err)
	}
	sessionID := createResp["session_id"]
	if sessionID == "" {
		t.Fatal("expected non-empty session_id")
	}

	// Verify the session exists in the store.
	sess, err := st.GetSession(sessionID)
	if err != nil {
		t.Fatalf("session not found in store: %v", err)
	}
	if sess.ID != sessionID {
		t.Errorf("expected session ID %q, got %q", sessionID, sess.ID)
	}

	// Call getOrCreateAgent twice and verify the same agent is returned.
	ag1, err := srv.getOrCreateAgent(sessionID)
	if err != nil {
		t.Fatalf("first getOrCreateAgent: %v", err)
	}
	ag2, err := srv.getOrCreateAgent(sessionID)
	if err != nil {
		t.Fatalf("second getOrCreateAgent: %v", err)
	}
	if ag1 != ag2 {
		t.Error("expected same agent instance on repeated getOrCreateAgent calls")
	}
}

func TestServer_CancelRunningAgent(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	srv.SetAgentFactory(stubAgentFactory())

	sess, _ := st.CreateSession("/tmp/test", "model-a")

	t.Run("cancel with no active agent returns 404", func(t *testing.T) {
		req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/cancel", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when no agent exists, got %d", w.Code)
		}
	})

	t.Run("cancel with active agent returns 200", func(t *testing.T) {
		// Create the agent first by calling getOrCreateAgent.
		_, err := srv.getOrCreateAgent(sess.ID)
		if err != nil {
			t.Fatalf("getOrCreateAgent: %v", err)
		}

		req := newAuthedRequest(srv, "POST", "/api/sessions/"+sess.ID+"/cancel", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 when agent exists, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]string
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp["status"] != "canceled" {
			t.Errorf("expected status 'cancelled', got %q", resp["status"])
		}
	})
}

func TestServer_ConfigSetDisabledTools(t *testing.T) {
	srv, st := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	srv.SetAgentFactory(stubAgentFactory())

	// Set up a provider so configureAgent doesn't panic on nil provider.
	anthropicProv, err := provider.GetProvider("anthropic")
	if err != nil {
		t.Fatalf("getting anthropic provider: %v", err)
	}
	srv.provider = anthropicProv

	// Create a session and agent first.
	sess, _ := st.CreateSession("/tmp/test", "test-model")
	ag, err := srv.getOrCreateAgent(sess.ID)
	if err != nil {
		t.Fatalf("getOrCreateAgent: %v", err)
	}
	_ = ag

	// Now set tools.disabled via POST /api/config.
	body, _ := json.Marshal(map[string]string{
		"key":   "tools.disabled",
		"value": "bash,file_write",
	})
	req := newAuthedRequest(srv, "POST", "/api/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the preference was set.
	if srv.prefs.ToolsDisabled != "bash,file_write" {
		t.Errorf("expected ToolsDisabled='bash,file_write', got %q", srv.prefs.ToolsDisabled)
	}

	// Verify DisabledToolsSet returns the correct set.
	disabled := srv.prefs.DisabledToolsSet()
	if !disabled["bash"] {
		t.Error("expected 'bash' in disabled tools set")
	}
	if !disabled["file_write"] {
		t.Error("expected 'file_write' in disabled tools set")
	}

	// Verify the response contains a success message.
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
}
