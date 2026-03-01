package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func (h *Hub) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", h.handleHealth)
	mux.HandleFunc("POST /api/hub/nodes/register", h.withAuth(h.handleRegisterNode))
	mux.HandleFunc("DELETE /api/hub/nodes/{id}", h.withAuth(h.handleDeregisterNode))
	mux.HandleFunc("GET /api/hub/nodes", h.withAuth(h.handleListNodes))
	mux.HandleFunc("GET /api/hub/nodes/{id}", h.withAuth(h.handleGetNode))
	mux.HandleFunc("POST /api/hub/nodes/{id}/heartbeat", h.withAuth(h.handleHeartbeat))
	mux.HandleFunc("GET /api/hub/sessions", h.withAuth(h.handleAggregatedSessions))
	mux.HandleFunc("POST /api/hub/logs", h.withAuth(h.handleIngestLog))
	mux.HandleFunc("GET /api/hub/logs/stream", h.withAuth(h.handleLogStream))
	// Proxy routes — match any method via wildcard
	mux.HandleFunc("/api/hub/proxy/{nodeID}/{path...}", h.withAuth(h.handleProxy))
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (h *Hub) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeHubJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"mode":   "hub",
		"pid":    os.Getpid(),
		"port":   h.port,
	})
}

// ---------------------------------------------------------------------------
// Node registration
// ---------------------------------------------------------------------------

type registerRequest struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	Version string `json:"version"`
}

func (h *Hub) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHubJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.Host == "" || req.Port == 0 || req.Token == "" {
		writeHubJSON(w, http.StatusBadRequest, map[string]string{"error": "host, port, and token are required"})
		return
	}
	node, err := h.registerNode(req.Name, req.Host, req.Port, req.Token, req.Version)
	if err != nil {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeHubJSON(w, http.StatusCreated, map[string]string{"id": node.ID})
}

func (h *Hub) handleDeregisterNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.getNode(id) == nil {
		writeHubJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	if err := h.deregisterNode(id); err != nil {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeHubJSON(w, http.StatusOK, map[string]string{"status": "deregistered"})
}

func (h *Hub) handleListNodes(w http.ResponseWriter, _ *http.Request) {
	nodes := h.listNodes()
	writeHubJSON(w, http.StatusOK, nodes)
}

func (h *Hub) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node := h.getNode(id)
	if node == nil {
		writeHubJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	writeHubJSON(w, http.StatusOK, node)
}

func (h *Hub) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.getNode(id) == nil {
		writeHubJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	if err := h.touchNode(id); err != nil {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeHubJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Session aggregation
// ---------------------------------------------------------------------------

type aggregatedSession struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	// Embed the raw session JSON from each node
	Session json.RawMessage `json:"session"`
}

func (h *Hub) handleAggregatedSessions(w http.ResponseWriter, r *http.Request) {
	nodes := h.listNodes()
	var results []aggregatedSession

	client := &http.Client{Timeout: 5 * time.Second}
	for _, node := range nodes {
		if node.Status != StatusOnline {
			continue
		}
		url := fmt.Sprintf("http://%s:%d/api/sessions", node.Host, node.Port)
		req, err := http.NewRequestWithContext(r.Context(), "GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+node.Token)
		resp, err := client.Do(req)
		if err != nil {
			h.logf("session aggregation: node %s: %v", node.ID, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		// body is a JSON array of sessions
		var sessions []json.RawMessage
		if err := json.Unmarshal(body, &sessions); err != nil {
			continue
		}
		for _, s := range sessions {
			results = append(results, aggregatedSession{
				NodeID:   node.ID,
				NodeName: node.Name,
				Session:  s,
			})
		}
	}
	if results == nil {
		results = []aggregatedSession{}
	}
	writeHubJSON(w, http.StatusOK, results)
}

// ---------------------------------------------------------------------------
// Log ingestion + streaming
// ---------------------------------------------------------------------------

type ingestLogRequest struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	NodeID  string `json:"node_id"`
}

func (h *Hub) handleIngestLog(w http.ResponseWriter, r *http.Request) {
	var req ingestLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHubJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.Message == "" {
		writeHubJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	level := req.Level
	if level == "" {
		level = "info"
	}

	// Look up node name
	nodeName := ""
	if req.NodeID != "" {
		if n := h.getNode(req.NodeID); n != nil {
			nodeName = n.Name
		}
	}

	id := generateLogID()
	now := time.Now().UTC()
	_, err := h.db.Exec(
		`INSERT INTO hub_logs (id, node_id, level, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, req.NodeID, level, req.Message, now.Format(time.RFC3339),
	)
	if err != nil {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	entry := LogEntry{
		NodeID:    req.NodeID,
		NodeName:  nodeName,
		Level:     level,
		Message:   req.Message,
		CreatedAt: now,
	}
	h.logBroker.publish(entry)
	writeHubJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Hub) handleLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeHubJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ch := h.logBroker.subscribe()
	defer h.logBroker.unsubscribe(ch)

	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func generateLogID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
