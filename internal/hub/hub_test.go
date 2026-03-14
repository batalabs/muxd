package hub

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateHub(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateHub_MemoryTable(t *testing.T) {
	db := testDB(t)

	// Insert
	_, err := db.Exec(`INSERT INTO memory (key, value) VALUES ('test_key', 'test_value')`)
	if err != nil {
		t.Fatalf("insert into memory: %v", err)
	}

	// Read back
	var val string
	err = db.QueryRow(`SELECT value FROM memory WHERE key = ?`, "test_key").Scan(&val)
	if err != nil {
		t.Fatalf("select from memory: %v", err)
	}
	if val != "test_value" {
		t.Errorf("expected test_value, got %s", val)
	}

	// Upsert
	_, err = db.Exec(`INSERT INTO memory (key, value, updated_at) VALUES ('test_key', 'updated', datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	err = db.QueryRow(`SELECT value FROM memory WHERE key = ?`, "test_key").Scan(&val)
	if err != nil {
		t.Fatalf("select after upsert: %v", err)
	}
	if val != "updated" {
		t.Errorf("expected updated, got %s", val)
	}
}

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	db := testDB(t)
	return &Hub{
		db:        db,
		token:     "test-token",
		nodes:     make(map[string]*Node),
		logBroker: newLogBroker(),
		ready:     make(chan struct{}),
		done:      make(chan struct{}),
	}
}

func TestHub_MemoryGetPut(t *testing.T) {
	h := newTestHub(t)

	// PUT memory
	body, _ := json.Marshal(map[string]any{
		"facts": map[string]string{"stack": "Go", "db": "SQLite"},
	})
	req := httptest.NewRequest("PUT", "/api/hub/memory", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handlePutMemory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT memory: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// GET memory
	req = httptest.NewRequest("GET", "/api/hub/memory", nil)
	w = httptest.NewRecorder()
	h.handleGetMemory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET memory: expected 200, got %d", w.Code)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["stack"] != "Go" || result["db"] != "SQLite" {
		t.Errorf("unexpected memory: %v", result)
	}

	// PUT with empty value to delete
	body, _ = json.Marshal(map[string]any{
		"facts": map[string]string{"db": ""},
	})
	req = httptest.NewRequest("PUT", "/api/hub/memory", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.handlePutMemory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT delete: expected 200, got %d", w.Code)
	}

	// Verify deletion
	req = httptest.NewRequest("GET", "/api/hub/memory", nil)
	w = httptest.NewRecorder()
	h.handleGetMemory(w, req)
	result = make(map[string]string)
	json.NewDecoder(w.Body).Decode(&result)
	if _, ok := result["db"]; ok {
		t.Error("expected db key to be deleted")
	}
	if result["stack"] != "Go" {
		t.Error("expected stack to remain")
	}
}

func TestHub_MemoryPut_emptyFacts(t *testing.T) {
	h := newTestHub(t)

	body, _ := json.Marshal(map[string]any{"facts": map[string]string{}})
	req := httptest.NewRequest("PUT", "/api/hub/memory", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handlePutMemory(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty facts, got %d", w.Code)
	}
}

func TestNodeClient_FetchMemory(t *testing.T) {
	facts := map[string]string{"stack": "Go", "db": "SQLite"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/memory" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer hub-tok" {
			t.Error("missing auth header")
		}
		json.NewEncoder(w).Encode(facts)
	}))
	defer srv.Close()

	c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
	got, err := c.FetchMemory()
	if err != nil {
		t.Fatal(err)
	}
	if got["stack"] != "Go" || got["db"] != "SQLite" {
		t.Errorf("unexpected facts: %v", got)
	}
}

func TestNodeClient_PushMemory(t *testing.T) {
	var received map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/memory" || r.Method != "PUT" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			Facts map[string]string `json:"facts"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		received = req.Facts
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
	err := c.PushMemory(map[string]string{"new_key": "new_value"})
	if err != nil {
		t.Fatal(err)
	}
	if received["new_key"] != "new_value" {
		t.Errorf("unexpected pushed facts: %v", received)
	}
}

// ---------------------------------------------------------------------------
// Node Registration & Lifecycle
// ---------------------------------------------------------------------------

func TestHub_RegisterNode(t *testing.T) {
	h := newTestHub(t)

	node, err := h.registerNode("node-a", "127.0.0.1", 9000, "tok-a", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("registerNode: %v", err)
	}
	if node.ID == "" {
		t.Fatal("expected non-empty node ID")
	}
	if node.Name != "node-a" {
		t.Errorf("expected name node-a, got %s", node.Name)
	}
	if node.Status != StatusOnline {
		t.Errorf("expected status online, got %s", node.Status)
	}

	nodes := h.listNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != node.ID {
		t.Errorf("listed node ID mismatch: got %s, want %s", nodes[0].ID, node.ID)
	}
}

func TestHub_RegisterNode_reregister(t *testing.T) {
	h := newTestHub(t)

	node1, err := h.registerNode("same-name", "127.0.0.1", 9000, "tok-1", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	node2, err := h.registerNode("same-name", "127.0.0.1", 9001, "tok-2", "0.2.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("second register: %v", err)
	}

	// Should reuse the same ID, not create a duplicate.
	if node2.ID != node1.ID {
		t.Errorf("expected same ID on re-register, got %s and %s", node1.ID, node2.ID)
	}

	nodes := h.listNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node after re-register, got %d", len(nodes))
	}

	// Verify updated fields.
	n := h.getNode(node1.ID)
	if n.Port != 9001 {
		t.Errorf("expected port 9001 after re-register, got %d", n.Port)
	}
	if n.Version != "0.2.0" {
		t.Errorf("expected version 0.2.0 after re-register, got %s", n.Version)
	}
}

func TestHub_DeregisterNode(t *testing.T) {
	h := newTestHub(t)

	node, err := h.registerNode("node-b", "127.0.0.1", 9000, "tok-b", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("registerNode: %v", err)
	}

	if err := h.deregisterNode(node.ID); err != nil {
		t.Fatalf("deregisterNode: %v", err)
	}

	if got := h.getNode(node.ID); got != nil {
		t.Error("expected node to be gone after deregister")
	}

	nodes := h.listNodes()
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after deregister, got %d", len(nodes))
	}
}

func TestHub_DeregisterNode_notFound(t *testing.T) {
	h := newTestHub(t)

	// deregisterNode on a non-existent ID should not return an error
	// (the DELETE SQL succeeds even if no rows match).
	err := h.deregisterNode("non-existent-id")
	if err != nil {
		t.Errorf("expected no error for deregistering non-existent node, got: %v", err)
	}
}

func TestHub_TouchNode(t *testing.T) {
	h := newTestHub(t)

	node, err := h.registerNode("node-c", "127.0.0.1", 9000, "tok-c", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("registerNode: %v", err)
	}

	originalLastSeen := node.LastSeenAt

	// Small sleep to ensure time difference.
	// We can't avoid this since touchNode uses time.Now().
	if err := h.touchNode(node.ID, NodeCapabilities{}); err != nil {
		t.Fatalf("touchNode: %v", err)
	}

	updated := h.getNode(node.ID)
	if updated == nil {
		t.Fatal("node not found after touch")
	}
	if !updated.LastSeenAt.After(originalLastSeen) && !updated.LastSeenAt.Equal(originalLastSeen) {
		t.Errorf("expected LastSeenAt to be >= original, got %v <= %v", updated.LastSeenAt, originalLastSeen)
	}
	if updated.Status != StatusOnline {
		t.Errorf("expected status online after touch, got %s", updated.Status)
	}
}

// ---------------------------------------------------------------------------
// Auth Middleware
// ---------------------------------------------------------------------------

func TestHub_WithAuth_validToken(t *testing.T) {
	h := newTestHub(t)

	called := false
	handler := h.withAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("expected handler to be called with valid token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHub_WithAuth_missingToken(t *testing.T) {
	h := newTestHub(t)

	called := false
	handler := h.withAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/test", nil)
	// No Authorization header
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called without auth header")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHub_WithAuth_wrongToken(t *testing.T) {
	h := newTestHub(t)

	called := false
	handler := h.withAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called with wrong token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHub_WithAuth_emptyBearerPrefix(t *testing.T) {
	h := newTestHub(t)

	called := false
	handler := h.withAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called with empty bearer token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Health Endpoint
// ---------------------------------------------------------------------------

func TestHub_HandleHealth(t *testing.T) {
	h := newTestHub(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	h.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, key := range []string{"status", "mode", "pid", "port"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("expected key %q in health response", key)
		}
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	if resp["mode"] != "hub" {
		t.Errorf("expected mode hub, got %v", resp["mode"])
	}
}

// ---------------------------------------------------------------------------
// Node List/Get Endpoints (via mux for PathValue support)
// ---------------------------------------------------------------------------

func newTestMux(h *Hub) *http.ServeMux {
	mux := http.NewServeMux()
	h.registerRoutes(mux)
	return mux
}

func TestHub_HandleListNodes(t *testing.T) {
	h := newTestHub(t)

	// Register two nodes.
	n1, err := h.registerNode("alpha", "127.0.0.1", 8001, "tok-a", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("register node 1: %v", err)
	}
	n2, err := h.registerNode("beta", "127.0.0.1", 8002, "tok-b", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("register node 2: %v", err)
	}

	mux := newTestMux(h)
	req := httptest.NewRequest("GET", "/api/hub/nodes", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var nodes []Node
	if err := json.NewDecoder(w.Body).Decode(&nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	ids := map[string]bool{n1.ID: false, n2.ID: false}
	for _, n := range nodes {
		ids[n.ID] = true
	}
	for id, found := range ids {
		if !found {
			t.Errorf("node %s not found in list response", id)
		}
	}
}

func TestHub_HandleGetNode(t *testing.T) {
	h := newTestHub(t)

	node, err := h.registerNode("gamma", "127.0.0.1", 8003, "tok-g", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	mux := newTestMux(h)
	req := httptest.NewRequest("GET", "/api/hub/nodes/"+node.ID, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got Node
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, got.ID)
	}
	if got.Name != "gamma" {
		t.Errorf("expected name gamma, got %s", got.Name)
	}
}

func TestHub_HandleGetNode_notFound(t *testing.T) {
	h := newTestHub(t)

	mux := newTestMux(h)
	req := httptest.NewRequest("GET", "/api/hub/nodes/nonexistent-id", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Log Ingestion
// ---------------------------------------------------------------------------

func TestHub_HandleIngestLog(t *testing.T) {
	h := newTestHub(t)

	body, _ := json.Marshal(ingestLogRequest{
		Level:   "info",
		Message: "hello from node",
		NodeID:  "some-node",
	})

	mux := newTestMux(h)
	req := httptest.NewRequest("POST", "/api/hub/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it was stored in the database.
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM hub_logs WHERE message = ?`, "hello from node").Scan(&count); err != nil {
		t.Fatalf("query hub_logs: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 log entry, got %d", count)
	}
}

func TestHub_HandleIngestLog_emptyMessage(t *testing.T) {
	h := newTestHub(t)

	body, _ := json.Marshal(ingestLogRequest{
		Level:   "info",
		Message: "",
		NodeID:  "some-node",
	})

	mux := newTestMux(h)
	req := httptest.NewRequest("POST", "/api/hub/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty message, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Proxy Handler
// ---------------------------------------------------------------------------

func TestHub_HandleProxy_nodeNotFound(t *testing.T) {
	h := newTestHub(t)

	mux := newTestMux(h)
	req := httptest.NewRequest("GET", "/api/hub/proxy/nonexistent-node/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHub_HandleProxy_nodeOffline(t *testing.T) {
	h := newTestHub(t)

	node, err := h.registerNode("offline-node", "127.0.0.1", 9999, "tok-off", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Mark the node offline.
	h.mu.Lock()
	h.nodes[node.ID].Status = StatusOffline
	h.mu.Unlock()

	mux := newTestMux(h)
	req := httptest.NewRequest("GET", "/api/hub/proxy/"+node.ID+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// SweepOfflineNodes
// ---------------------------------------------------------------------------

func TestHub_HandleHeartbeat_unknownNode(t *testing.T) {
	h := newTestHub(t)

	mux := newTestMux(h)
	req := httptest.NewRequest("POST", "/api/hub/nodes/nonexistent-id/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown node heartbeat, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNodeClient_Heartbeat_404_returnsNodePurged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeHubJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
	}))
	defer srv.Close()

	c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
	err := c.Heartbeat("some-node-id")
	if err == nil {
		t.Fatal("expected error for 404 heartbeat")
	}
	if !IsNodePurgedError(err) {
		t.Errorf("expected node purged error, got: %v", err)
	}
}

func TestNodeClient_Heartbeat_500_notPurgedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": "db busy"})
	}))
	defer srv.Close()

	c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
	err := c.Heartbeat("some-node-id")
	if err == nil {
		t.Fatal("expected error for 500 heartbeat")
	}
	if IsNodePurgedError(err) {
		t.Error("500 error should not be a node-purged error")
	}
}

func TestHub_SweepOfflineNodes(t *testing.T) {
	h := newTestHub(t)

	node, err := h.registerNode("stale-node", "127.0.0.1", 9000, "tok-s", "0.1.0", NodeCapabilities{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Manually backdate LastSeenAt to exceed the 90s offline cutoff.
	oldTime := time.Now().UTC().Add(-2 * time.Minute)
	h.mu.Lock()
	h.nodes[node.ID].LastSeenAt = oldTime
	h.mu.Unlock()

	h.sweepOfflineNodes()

	n := h.getNode(node.ID)
	if n == nil {
		t.Fatal("node should still exist after sweep (just marked offline)")
	}
	if n.Status != StatusOffline {
		t.Errorf("expected node status offline after sweep, got %s", n.Status)
	}
}

// ---------------------------------------------------------------------------
// Task 2: NodeClient.resolveNodeID
// ---------------------------------------------------------------------------

func makeNodeListServer(t *testing.T, nodes []NodeListEntry) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/nodes" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(nodes)
	}))
}

func TestNodeClient_resolveNodeID(t *testing.T) {
	nodes := []NodeListEntry{
		{ID: "abc-123", Name: "linux-node", Status: "online"},
		{ID: "def-456", Name: "Win-Node", Status: "online"},
		{ID: "ghi-789", Name: "offline-node", Status: "offline"},
	}

	tests := []struct {
		name      string
		input     string
		wantID    string
		wantErr   string
	}{
		{
			name:   "resolve by exact ID",
			input:  "abc-123",
			wantID: "abc-123",
		},
		{
			name:   "resolve by name (exact case)",
			input:  "linux-node",
			wantID: "abc-123",
		},
		{
			name:   "resolve by name (case insensitive)",
			input:  "win-node",
			wantID: "def-456",
		},
		{
			name:    "offline node returns error with not online",
			input:   "offline-node",
			wantErr: "not online",
		},
		{
			name:    "not found returns error with not found",
			input:   "missing-node",
			wantErr: "not found",
		},
	}

	srv := makeNodeListServer(t, nodes)
	defer srv.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
			gotID, err := c.resolveNodeID(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotID != tt.wantID {
				t.Errorf("expected ID %q, got %q", tt.wantID, gotID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 3: NodeClient.proxyCreateSession
// ---------------------------------------------------------------------------

func TestNodeClient_proxyCreateSession(t *testing.T) {
	tests := []struct {
		name          string
		nodeID        string
		serverStatus  int
		sessionID     string
		wantErr       bool
		checkRequest  bool
	}{
		{
			name:         "success: correct path auth body returns session_id",
			nodeID:       "node-abc",
			serverStatus: http.StatusOK,
			sessionID:    "sess-xyz-789",
			checkRequest: true,
		},
		{
			name:         "server error 500 returns error",
			nodeID:       "node-abc",
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := fmt.Sprintf("/api/hub/proxy/%s/api/sessions", tt.nodeID)
				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path: got %q, want %q", r.URL.Path, expectedPath)
				}
				if r.Method != "POST" {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if tt.checkRequest {
					authHeader := r.Header.Get("Authorization")
					if !strings.HasPrefix(authHeader, "Bearer ") {
						t.Errorf("expected Bearer auth header, got %q", authHeader)
					}

					var body map[string]string
					json.NewDecoder(r.Body).Decode(&body)
					if body["project_path"] != "__hub_dispatch__" {
						t.Errorf("expected project_path __hub_dispatch__, got %q", body["project_path"])
					}
				}
				w.WriteHeader(tt.serverStatus)
				if tt.serverStatus == http.StatusOK {
					json.NewEncoder(w).Encode(map[string]string{"session_id": tt.sessionID})
				} else {
					json.NewEncoder(w).Encode(map[string]string{"error": "internal server error"})
				}
			}))
			defer srv.Close()

			c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
			gotID, err := c.proxyCreateSession(tt.nodeID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotID != tt.sessionID {
				t.Errorf("expected session_id %q, got %q", tt.sessionID, gotID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 4: NodeClient.proxySubmit
// ---------------------------------------------------------------------------

// buildSSEStream builds a minimal SSE response body from a slice of events.
// Each event is [type, jsonData].
func buildSSEStream(events [][2]string) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e[0], e[1])
	}
	return b.String()
}

func TestNodeClient_proxySubmit(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		sseEvents  [][2]string
		wantResult string
		wantErr    string
	}{
		{
			name: "collects delta text from SSE stream",
			sseEvents: [][2]string{
				{"delta", `{"text":"Hello "}`},
				{"delta", `{"text":"world!"}`},
				{"turn_done", `{"stop_reason":"end_turn"}`},
			},
			wantResult: "Hello world!",
		},
		{
			name: "captures error events in output",
			sseEvents: [][2]string{
				{"delta", `{"text":"Partial"}`},
				{"error", `{"error":"something went wrong"}`},
				{"turn_done", `{"stop_reason":"error"}`},
			},
			wantResult: "Partial\nError: something went wrong",
		},
		{
			name:    "HTTP error returns error",
			status:  http.StatusInternalServerError,
			wantErr: "HTTP 500",
		},
		{
			name: "output truncated at 50KB via large single chunk",
			sseEvents: func() [][2]string {
				// One delta that is 60KB — exceeds 50KB limit.
				// The guard is output.Len() < maxOutput, so this single chunk
				// (written when output is empty) pushes output past 50KB,
				// triggering the post-write truncation to maxOutput bytes.
				chunk := strings.Repeat("x", 60*1024)
				jsonData := fmt.Sprintf(`{"text":%q}`, chunk)
				return [][2]string{
					{"delta", jsonData},
					{"turn_done", `{"stop_reason":"end_turn"}`},
				}
			}(),
			wantResult: "truncated at 50KB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedStatus := http.StatusOK
			if tt.status != 0 {
				expectedStatus = tt.status
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(expectedStatus)
				if expectedStatus == http.StatusOK {
					body := buildSSEStream(tt.sseEvents)
					w.Write([]byte(body))
				} else {
					w.Write([]byte("internal error"))
				}
			}))
			defer srv.Close()

			c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
			result, err := c.proxySubmit("node-id", "sess-id", "do something")

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, tt.wantResult) {
				t.Errorf("expected result containing %q, got %q", tt.wantResult, result)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 5: NodeClient.Dispatch integration test
// ---------------------------------------------------------------------------

func TestNodeClient_Dispatch(t *testing.T) {
	type testNode struct {
		id     string
		name   string
		status string
	}

	nodes := []testNode{
		{id: "node-001", name: "worker", status: "online"},
		{id: "node-002", name: "offline-worker", status: "offline"},
	}

	buildServer := func(t *testing.T, sseBody string) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/hub/nodes" && r.Method == "GET":
				// List nodes
				var list []NodeListEntry
				for _, n := range nodes {
					list = append(list, NodeListEntry{
						ID:     n.id,
						Name:   n.name,
						Status: n.status,
					})
				}
				json.NewEncoder(w).Encode(list)

			case strings.HasSuffix(r.URL.Path, "/api/sessions") && r.Method == "POST":
				// Create session — extract nodeID from path
				json.NewEncoder(w).Encode(map[string]string{"session_id": "sess-dispatch-001"})

			case strings.Contains(r.URL.Path, "/api/sessions/") && strings.HasSuffix(r.URL.Path, "/submit") && r.Method == "POST":
				// Submit prompt — return SSE stream
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(sseBody))

			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "not found", http.StatusNotFound)
			}
		}))
	}

	sseBody := buildSSEStream([][2]string{
		{"delta", `{"text":"Task done!"}`},
		{"turn_done", `{"stop_reason":"end_turn"}`},
	})

	t.Run("dispatch by name", func(t *testing.T) {
		srv := buildServer(t, sseBody)
		defer srv.Close()

		c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
		result, err := c.Dispatch("worker", "do the work")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Task done!") {
			t.Errorf("expected 'Task done!' in result, got %q", result)
		}
	})

	t.Run("dispatch by ID", func(t *testing.T) {
		srv := buildServer(t, sseBody)
		defer srv.Close()

		c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
		result, err := c.Dispatch("node-001", "do the work")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Task done!") {
			t.Errorf("expected 'Task done!' in result, got %q", result)
		}
	})

	t.Run("unknown node returns error", func(t *testing.T) {
		srv := buildServer(t, sseBody)
		defer srv.Close()

		c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
		_, err := c.Dispatch("nonexistent", "do the work")
		if err == nil {
			t.Fatal("expected error for unknown node, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got %q", err.Error())
		}
	})

	t.Run("offline node returns error", func(t *testing.T) {
		srv := buildServer(t, sseBody)
		defer srv.Close()

		c := NewNodeClient(srv.URL, "hub-tok", "node-tok")
		_, err := c.Dispatch("offline-worker", "do the work")
		if err == nil {
			t.Fatal("expected error for offline node, got nil")
		}
		if !strings.Contains(err.Error(), "not online") {
			t.Errorf("expected 'not online' in error, got %q", err.Error())
		}
	})
}
