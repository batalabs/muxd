package hub

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
