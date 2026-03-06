package hub

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
)

// NodeStatus represents the health state of a registered node.
type NodeStatus string

const (
	StatusOnline  NodeStatus = "online"
	StatusOffline NodeStatus = "offline"
)

// Node represents a registered muxd instance.
type Node struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Host         string     `json:"host"`
	Port         int        `json:"port"`
	Token        string     `json:"-"` // never sent to clients
	Version      string     `json:"version"`
	Status       NodeStatus `json:"status"`
	RegisteredAt time.Time  `json:"registered_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
}

// Hub is the central coordinator that tracks nodes, proxies requests,
// aggregates sessions, and streams logs.
type Hub struct {
	db        *sql.DB
	token     string
	prefs     *config.Preferences
	mu        sync.RWMutex
	nodes     map[string]*Node
	version   string
	logBroker *logBroker
	server    *http.Server
	port      int
	bindAddr  string
	ready     chan struct{}
	done      chan struct{}
	logger    *config.Logger
}

// NewHub creates a new Hub instance. Token resolution order:
//  1. explicitToken (from --hub-token flag or MUXD_HUB_TOKEN env var)
//  2. Token stored in the hub database (survives config.json loss)
//  3. Token from preferences (config.json)
//  4. Generate a fresh token
//
// The resolved token is always persisted back to the hub database so it
// survives even if config.json is deleted during a rebuild.
func NewHub(db *sql.DB, prefs *config.Preferences, logger *config.Logger, explicitToken string) *Hub {
	token := explicitToken
	if token == "" {
		token = GetSetting(db, "hub_auth_token")
	}
	if token == "" && prefs != nil {
		token = prefs.HubAuthToken
	}
	if token == "" {
		token = generateHubToken()
	}
	// Always persist to database so the token survives config.json loss.
	SetSetting(db, "hub_auth_token", token)

	h := &Hub{
		db:        db,
		token:     token,
		prefs:     prefs,
		nodes:     make(map[string]*Node),
		logBroker: newLogBroker(),
		ready:     make(chan struct{}),
		done:      make(chan struct{}),
		logger:    logger,
	}
	// Load existing nodes from the database into memory.
	h.loadNodes()
	return h
}

// AuthToken returns the hub's bearer token.
func (h *Hub) AuthToken() string { return h.token }

// Port returns the port the hub is listening on. Blocks until Start() has bound.
func (h *Hub) Port() int {
	<-h.ready
	return h.port
}

// SetVersion sets the version string reported by the health endpoint.
func (h *Hub) SetVersion(v string) { h.version = v }

// SetBindAddress sets the network interface to bind to.
func (h *Hub) SetBindAddress(addr string) { h.bindAddr = addr }

// Start begins serving the hub HTTP API on the given port.
func (h *Hub) Start(port int) error {
	bindAddr := h.bindAddr
	if bindAddr == "" {
		bindAddr = "localhost"
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindAddr, port))
	if err != nil {
		ln, err = net.Listen("tcp", fmt.Sprintf("%s:0", bindAddr))
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
	}
	h.port = ln.Addr().(*net.TCPAddr).Port
	h.logf("hub starting on %s:%d", bindAddr, h.port)
	fmt.Fprintf(os.Stderr, "muxd hub listening on %s:%d\n", bindAddr, h.port)
	h.printConnectionQR(bindAddr)
	close(h.ready)

	if err := writeHubLockfile(h.port, h.token, bindAddr); err != nil {
		ln.Close()
		return fmt.Errorf("writing hub lockfile: %w", err)
	}

	go h.startHealthChecker()

	mux := http.NewServeMux()
	h.registerRoutes(mux)
	h.server = &http.Server{Handler: mux}
	if err := h.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully stops the hub server.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.logf("hub shutting down")
	close(h.done)
	var err error
	if h.server != nil {
		err = h.server.Shutdown(ctx)
	}
	if rmErr := removeHubLockfile(); rmErr != nil {
		h.logf("removing hub lockfile: %v", rmErr)
	}
	return err
}

// ---------------------------------------------------------------------------
// Node registry
// ---------------------------------------------------------------------------

func (h *Hub) registerNode(name, host string, port int, token, version string) (*Node, error) {
	now := time.Now().UTC()

	// Check for an existing node with the same name -replace it instead of
	// creating a duplicate. This handles daemon restarts cleanly.
	if existingID := h.findNodeByName(name); existingID != "" {
		h.mu.Lock()
		if n, ok := h.nodes[existingID]; ok {
			n.Host = host
			n.Port = port
			n.Token = token
			n.Version = version
			n.Status = StatusOnline
			n.LastSeenAt = now
		}
		h.mu.Unlock()
		h.db.Exec(
			`UPDATE nodes SET host = ?, port = ?, token = ?, version = ?, status = ?, last_seen_at = ? WHERE id = ?`,
			host, port, token, version, string(StatusOnline), now.Format(time.RFC3339), existingID,
		)
		h.logf("node re-registered: %s (%s:%d)", existingID, host, port)
		return h.getNode(existingID), nil
	}

	id := generateNodeID()
	node := &Node{
		ID:           id,
		Name:         name,
		Host:         host,
		Port:         port,
		Token:        token,
		Version:      version,
		Status:       StatusOnline,
		RegisteredAt: now,
		LastSeenAt:   now,
	}

	_, err := h.db.Exec(
		`INSERT INTO nodes (id, name, host, port, token, version, status, registered_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.Name, node.Host, node.Port, node.Token, node.Version,
		string(node.Status),
		node.RegisteredAt.Format(time.RFC3339),
		node.LastSeenAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("inserting node: %w", err)
	}

	h.mu.Lock()
	h.nodes[id] = node
	h.mu.Unlock()

	h.logf("node registered: %s (%s:%d)", id, host, port)
	return node, nil
}

func (h *Hub) findNodeByName(name string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, n := range h.nodes {
		if n.Name == name {
			return n.ID
		}
	}
	return ""
}

func (h *Hub) deregisterNode(id string) error {
	_, err := h.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}
	h.mu.Lock()
	delete(h.nodes, id)
	h.mu.Unlock()
	h.logf("node deregistered: %s", id)
	return nil
}

func (h *Hub) touchNode(id string) error {
	now := time.Now().UTC()
	_, err := h.db.Exec(
		`UPDATE nodes SET last_seen_at = ?, status = ? WHERE id = ?`,
		now.Format(time.RFC3339), string(StatusOnline), id,
	)
	if err != nil {
		return fmt.Errorf("touching node: %w", err)
	}
	h.mu.Lock()
	if n, ok := h.nodes[id]; ok {
		n.LastSeenAt = now
		n.Status = StatusOnline
	}
	h.mu.Unlock()
	return nil
}

func (h *Hub) getNode(id string) *Node {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.nodes[id]
}

func (h *Hub) listNodes() []*Node {
	h.mu.RLock()
	defer h.mu.RUnlock()
	nodes := make([]*Node, 0, len(h.nodes))
	for _, n := range h.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// ---------------------------------------------------------------------------
// Health checker
// ---------------------------------------------------------------------------

func (h *Hub) startHealthChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.sweepOfflineNodes()
		case <-h.done:
			return
		}
	}
}

func (h *Hub) sweepOfflineNodes() {
	now := time.Now().UTC()
	offlineCutoff := now.Add(-90 * time.Second)
	purgeCutoff := now.Add(-1 * time.Hour)
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, n := range h.nodes {
		if n.Status == StatusOnline && n.LastSeenAt.Before(offlineCutoff) {
			n.Status = StatusOffline
			h.db.Exec(
				`UPDATE nodes SET status = ? WHERE id = ?`,
				string(StatusOffline), n.ID,
			)
			h.logf("node %s marked offline (last seen %s)", n.ID, n.LastSeenAt.Format(time.RFC3339))
		} else if n.Status == StatusOffline && n.LastSeenAt.Before(purgeCutoff) {
			// Purge nodes that have been offline for over 1 hour.
			h.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
			delete(h.nodes, id)
			h.logf("node %s purged (offline since %s)", id, n.LastSeenAt.Format(time.RFC3339))
		}
	}
}

// ---------------------------------------------------------------------------
// Load persisted nodes
// ---------------------------------------------------------------------------

func (h *Hub) loadNodes() {
	// Purge stale nodes on startup -nodes offline for over 1 hour.
	purgeCutoff := time.Now().UTC().Add(-1 * time.Hour)
	h.db.Exec(`DELETE FROM nodes WHERE status = ? AND last_seen_at < ?`,
		string(StatusOffline), purgeCutoff.Format(time.RFC3339))

	// Mark all remaining nodes as offline on startup -they must re-register
	// or heartbeat to prove they're alive.
	h.db.Exec(`UPDATE nodes SET status = ?`, string(StatusOffline))

	rows, err := h.db.Query(`SELECT id, name, host, port, token, version, status, registered_at, last_seen_at FROM nodes`)
	if err != nil {
		h.logf("loading nodes: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var n Node
		var status, regAt, seenAt string
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.Token, &n.Version, &status, &regAt, &seenAt); err != nil {
			h.logf("scanning node row: %v", err)
			continue
		}
		n.Status = NodeStatus(status)
		n.RegisteredAt, _ = time.Parse(time.RFC3339, regAt)
		n.LastSeenAt, _ = time.Parse(time.RFC3339, seenAt)
		h.nodes[n.ID] = &n
	}
	h.logf("loaded %d nodes from database", len(h.nodes))
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

func (h *Hub) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if strings.HasPrefix(got, bearer) {
			got = strings.TrimSpace(strings.TrimPrefix(got, bearer))
		}
		if got == "" || h.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
			writeHubJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Hub) printConnectionQR(bindAddr string) {
	// Determine which host to encode -use LAN IP when bound to all interfaces.
	host := bindAddr
	if host == "0.0.0.0" || host == "" {
		if ips := daemon.GetLocalIPs(); len(ips) > 0 {
			host = ips[0]
		}
	}

	ascii, err := daemon.GenerateQRCodeASCII(host, h.port, h.token)
	if err != nil {
		h.logf("QR code generation failed: %v", err)
		return
	}

	fmt.Fprintf(os.Stderr, "\nScan to connect:\n%s\n", ascii)
	fmt.Fprintf(os.Stderr, "  hub:   %s:%d\n", host, h.port)
	fmt.Fprintf(os.Stderr, "  token: %s\n", h.token)
	if ips := daemon.GetLocalIPs(); len(ips) > 1 {
		fmt.Fprintf(os.Stderr, "  also available on:")
		for _, ip := range ips {
			if ip != host {
				fmt.Fprintf(os.Stderr, " %s", ip)
			}
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
	fmt.Fprintf(os.Stderr, "\n  connect: muxd --remote %s:%d --token %s\n\n", host, h.port, h.token)
}

func (h *Hub) logf(format string, args ...any) {
	if h.logger != nil {
		h.logger.Printf(format, args...)
	}
}

func generateHubToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func generateNodeID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func writeHubJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "hub: write json: %v\n", err)
	}
}

// ---------------------------------------------------------------------------
// Hub lockfile -separate from daemon lockfile
// ---------------------------------------------------------------------------

const hubLockfileName = "hub.lock"

func writeHubLockfile(port int, token, bindAddr string) error {
	dir, err := config.DataDir()
	if err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	data := map[string]any{
		"pid":        os.Getpid(),
		"port":       port,
		"bind_addr":  bindAddr,
		"token":      token,
		"started_at": time.Now().Format(time.RFC3339),
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling hub lockfile: %w", err)
	}
	return os.WriteFile(fmt.Sprintf("%s/%s", dir, hubLockfileName), b, 0o600)
}

// HubLockfile holds the data persisted in the hub lockfile.
type HubLockfile struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	BindAddr string `json:"bind_addr"`
	Token    string `json:"token"`
}

// ReadHubLockfile reads the hub lockfile from the data directory.
func ReadHubLockfile() (*HubLockfile, error) {
	dir, err := config.DataDir()
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	data, err := os.ReadFile(fmt.Sprintf("%s/%s", dir, hubLockfileName))
	if err != nil {
		return nil, fmt.Errorf("reading hub lockfile: %w", err)
	}
	var lf HubLockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing hub lockfile: %w", err)
	}
	return &lf, nil
}

func removeHubLockfile() error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	p := fmt.Sprintf("%s/%s", dir, hubLockfileName)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing hub lockfile: %w", err)
	}
	return nil
}
