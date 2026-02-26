package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/agent"
	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"
	"github.com/batalabs/muxd/internal/tools"
)

// AgentFactory creates an agent.Service for a session.
type AgentFactory func(apiKey, modelID, modelLabel string, st *store.Store, sess *domain.Session, prov provider.Provider) *agent.Service

// DetectGitRepoFunc detects the git repo root if inside one.
type DetectGitRepoFunc func() (string, bool)

// Server is the HTTP daemon that wraps agent.Service for each session.
type Server struct {
	store      *store.Store
	apiKey     string
	modelID    string
	modelLabel string
	provider   provider.Provider
	prefs      *config.Preferences

	mu       sync.Mutex
	agents   map[string]*agent.Service // sessionID -> agent
	askChans map[string]chan<- string  // askID -> response channel

	port   int
	ready  chan struct{} // closed once port is assigned in Start()
	server *http.Server
	quiet  bool
	token  string
	sched  *tools.ToolCallScheduler

	newAgent      AgentFactory
	detectGitRepo DetectGitRepoFunc
	mcpManager    *mcp.Manager
}

// NewServer creates a new daemon server.
func NewServer(st *store.Store, apiKey, modelID, modelLabel string, prov provider.Provider, prefs *config.Preferences) *Server {
	return &Server{
		store:      st,
		apiKey:     apiKey,
		modelID:    modelID,
		modelLabel: modelLabel,
		provider:   prov,
		prefs:      prefs,
		agents:     make(map[string]*agent.Service),
		askChans:   make(map[string]chan<- string),
		ready:      make(chan struct{}),
		token:      generateAuthToken(),
	}
}

func generateAuthToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; empty token means auth check will reject requests.
		return ""
	}
	return hex.EncodeToString(b[:])
}

// AuthToken returns the daemon auth token for trusted in-process callers.
func (s *Server) AuthToken() string {
	return s.token
}

// SetAgentFactory sets the factory used to create new agents. Must be called
// before Start() if agents should be usable.
func (s *Server) SetAgentFactory(f AgentFactory) {
	s.newAgent = f
}

// SetDetectGitRepo sets the function used to detect git repos.
func (s *Server) SetDetectGitRepo(f DetectGitRepoFunc) {
	s.detectGitRepo = f
}

// SetQuiet controls whether startup logs are suppressed.
func (s *Server) SetQuiet(quiet bool) {
	s.quiet = quiet
}

// initMCP loads .mcp.json config and starts MCP server connections.
func (s *Server) initMCP() {
	cwd, _ := tools.Getwd()
	cfg, err := mcp.LoadMCPConfig(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: config error: %v\n", err)
		return
	}
	if len(cfg.MCPServers) == 0 {
		return
	}

	mgr := mcp.NewManager()
	ctx := context.Background()
	if err := mgr.StartAll(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "mcp: startup error: %v\n", err)
	}
	s.mu.Lock()
	s.mcpManager = mgr
	// Propagate to any agents that were created before MCP init finished.
	for _, ag := range s.agents {
		ag.SetMCPManager(mgr)
	}
	s.mu.Unlock()

	names := mgr.ToolNames()
	if len(names) > 0 && !s.quiet {
		fmt.Fprintf(os.Stderr, "mcp: %d tools from %d servers\n", len(names), len(cfg.MCPServers))
	}
}

// Start begins listening on the given port. If the port is taken, falls back
// to an OS-assigned port. Blocks until the server shuts down.
func (s *Server) Start(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		// Port in use -- let OS assign
		ln, err = net.Listen("tcp", "localhost:0")
		if err != nil {
			return fmt.Errorf("listening: %w", err)
		}
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	if !s.quiet {
		fmt.Fprintf(os.Stderr, "muxd server listening on port %d\n", s.port)
	}
	close(s.ready) // signal that port is assigned

	if err := WriteLockfile(s.port, s.token); err != nil {
		ln.Close()
		return fmt.Errorf("writing lockfile: %w", err)
	}

	// Initialize MCP connections in background so it doesn't block HTTP serving.
	go s.initMCP()

	s.sched = tools.NewToolCallScheduler(
		daemonScheduledToolStore{st: s.store},
		time.Minute,
		func() *tools.ToolContext {
			cwd, _ := tools.Getwd()
			s.mu.Lock()
			defer s.mu.Unlock()
			disabled := map[string]bool{}
			allowed := map[string]bool{}
			braveKey := ""
			xClientID := ""
			xClientSecret := ""
			xAccessToken := ""
			xRefreshToken := ""
			xTokenExpiry := ""
			if s.prefs != nil {
				disabled = s.prefs.DisabledToolsSet()
				allowed = s.prefs.ScheduledAllowedToolsSet()
				braveKey = s.prefs.BraveAPIKey
				xClientID = s.prefs.XClientID
				xClientSecret = s.prefs.XClientSecret
				xAccessToken = s.prefs.XAccessToken
				xRefreshToken = s.prefs.XRefreshToken
				xTokenExpiry = s.prefs.XTokenExpiry
			}
			planMode := false
			ctx := &tools.ToolContext{
				Cwd:              cwd,
				PlanMode:         &planMode,
				Disabled:         disabled,
				ScheduledAllowed: allowed,
				BraveAPIKey:      braveKey,
				XClientID:        xClientID,
				XClientSecret:    xClientSecret,
				XAccessToken:     xAccessToken,
				XRefreshToken:    xRefreshToken,
				XTokenExpiry:     xTokenExpiry,
				SaveXOAuthTokens: func(accessToken, refreshToken, tokenExpiry string) error {
					s.mu.Lock()
					defer s.mu.Unlock()
					if s.prefs == nil {
						return fmt.Errorf("preferences not loaded")
					}
					s.prefs.XAccessToken = accessToken
					s.prefs.XRefreshToken = refreshToken
					s.prefs.XTokenExpiry = tokenExpiry
					return config.SavePreferences(*s.prefs)
				},
				ScheduleTool: s.store.CreateScheduledToolJob,
				ListScheduledJobs: func(toolName string, limit int) ([]tools.ScheduledJobInfo, error) {
					jobs, err := s.store.ListScheduledToolJobs(limit)
					if err != nil {
						return nil, err
					}
					var out []tools.ScheduledJobInfo
					for _, j := range jobs {
						if j.ToolName != toolName {
							continue
						}
						out = append(out, tools.ScheduledJobInfo{
							ID:           j.ID,
							ToolName:     j.ToolName,
							ToolInput:    j.ToolInput,
							ScheduledFor: j.ScheduledFor,
							Recurrence:   j.Recurrence,
							Status:       j.Status,
							CreatedAt:    j.CreatedAt,
						})
					}
					return out, nil
				},
				CancelScheduledJob: s.store.CancelScheduledToolJob,
				UpdateScheduledJob: s.store.UpdateScheduledToolJob,
			}
			return ctx
		},
		func(call tools.ScheduledToolCall, ctx *tools.ToolContext) (string, bool, error) {
			// Agent tasks spawn a full agent loop instead of a single tool call.
			if call.ToolName == tools.AgentTaskToolName {
				return s.executeScheduledAgentTask(call)
			}
			block := domain.ContentBlock{
				Type:      "tool_use",
				ToolUseID: call.ID,
				ToolName:  call.ToolName,
				ToolInput: call.ToolInput,
			}
			result, isErr := agent.ExecuteToolCall(block, ctx)
			return result, isErr, nil
		},
	)
	s.sched.Start()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{Handler: mux}
	if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully stops the server and removes the lockfile.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	mgr := s.mcpManager
	s.mu.Unlock()
	var err error
	if mgr != nil {
		mgr.StopAll()
	}
	if s.sched != nil {
		s.sched.Stop()
	}
	if s.server != nil {
		err = s.server.Shutdown(ctx)
	}
	if err := RemoveLockfile(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: remove lockfile: %v\n", err)
	}
	return err
}

type daemonScheduledToolStore struct {
	st *store.Store
}

func (d daemonScheduledToolStore) DueScheduledToolCalls(now time.Time, limit int) ([]tools.ScheduledToolCall, error) {
	items, err := d.st.DueScheduledToolJobs(now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]tools.ScheduledToolCall, 0, len(items))
	for _, it := range items {
		out = append(out, tools.ScheduledToolCall{
			ID:           it.ID,
			Source:       "tool_job",
			ToolName:     it.ToolName,
			ToolInput:    it.ToolInput,
			ScheduledFor: it.ScheduledFor,
			Recurrence:   it.Recurrence,
		})
	}

	// Compatibility path: also execute legacy scheduled tweets.
	tweets, err := d.st.DueScheduledTweets(now, limit)
	if err == nil {
		for _, tw := range tweets {
			out = append(out, tools.ScheduledToolCall{
				ID:       tw.ID,
				Source:   "legacy_tweet",
				ToolName: "x_post",
				ToolInput: map[string]any{
					"text": tw.Text,
				},
				ScheduledFor: tw.ScheduledFor,
				Recurrence:   tw.Recurrence,
			})
		}
	}
	return out, nil
}

func (d daemonScheduledToolStore) MarkScheduledToolCallSucceeded(call tools.ScheduledToolCall, result string, completedAt time.Time) error {
	if call.Source == "legacy_tweet" {
		tweetID := extractTweetID(result)
		// legacy schema stores tweet_id; parse when available from tool output.
		return d.st.MarkScheduledTweetPosted(call.ID, tweetID, completedAt)
	}
	return d.st.MarkScheduledToolJobSucceeded(call.ID, result, completedAt)
}

func (d daemonScheduledToolStore) MarkScheduledToolCallFailed(call tools.ScheduledToolCall, errText, result string, attemptedAt time.Time) error {
	if call.Source == "legacy_tweet" {
		return d.st.MarkScheduledTweetFailed(call.ID, errText, attemptedAt)
	}
	return d.st.MarkScheduledToolJobFailed(call.ID, errText, result, attemptedAt)
}

func (d daemonScheduledToolStore) RescheduleScheduledToolCall(call tools.ScheduledToolCall, next time.Time) error {
	if call.Source == "legacy_tweet" {
		return d.st.RescheduleRecurringTweet(call.ID, next)
	}
	return d.st.RescheduleScheduledToolJob(call.ID, next)
}

func extractTweetID(result string) string {
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) == 0 {
		return ""
	}
	parts := strings.Fields(lines[0])
	// Expected: "Posted tweet <id>"
	if len(parts) >= 3 && strings.EqualFold(parts[0], "posted") && strings.EqualFold(parts[1], "tweet") {
		return parts[2]
	}
	return ""
}

// Port returns the actual listening port. Blocks until Start() has bound the
// listener and assigned the port.
func (s *Server) Port() int {
	<-s.ready
	return s.port
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/sessions", s.withAuth(s.handleCreateSession))
	mux.HandleFunc("GET /api/sessions/{id}", s.withAuth(s.handleGetSession))
	mux.HandleFunc("DELETE /api/sessions/{id}", s.withAuth(s.handleDeleteSession))
	mux.HandleFunc("GET /api/sessions", s.withAuth(s.handleListSessions))
	mux.HandleFunc("POST /api/sessions/{id}/submit", s.withAuth(s.handleSubmit))
	mux.HandleFunc("POST /api/sessions/{id}/cancel", s.withAuth(s.handleCancel))
	mux.HandleFunc("POST /api/sessions/{id}/ask-response", s.withAuth(s.handleAskResponse))
	mux.HandleFunc("GET /api/sessions/{id}/messages", s.withAuth(s.handleGetMessages))
	mux.HandleFunc("POST /api/sessions/{id}/model", s.withAuth(s.handleSetModel))
	mux.HandleFunc("POST /api/sessions/{id}/title", s.withAuth(s.handleSetTitle))
	mux.HandleFunc("POST /api/sessions/{id}/branch", s.withAuth(s.handleBranch))
	mux.HandleFunc("POST /api/config", s.withAuth(s.handleSetConfig))
	mux.HandleFunc("GET /api/config", s.withAuth(s.handleGetConfig))
	mux.HandleFunc("GET /api/mcp/tools", s.withAuth(s.handleMCPTools))
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if strings.HasPrefix(got, bearer) {
			got = strings.TrimSpace(strings.TrimPrefix(got, bearer))
		}
		// Constant-time compare to avoid token oracle behavior.
		if got == "" || s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"pid":    os.Getpid(),
		"port":   s.port,
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectPath string `json:"project_path"`
		ModelID     string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	modelID := req.ModelID
	if modelID == "" {
		modelID = s.modelID
	}
	sess, err := s.store.CreateSession(req.ProjectPath, modelID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sess.ID})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.GetSession(id)
	if err != nil {
		// Try prefix match
		sess, err = s.store.FindSessionByPrefix(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Try to find the session first (supports prefix match)
	sess, err := s.store.GetSession(id)
	if err != nil {
		sess, err = s.store.FindSessionByPrefix(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
	}

	// Clean up any active agent for this session
	s.mu.Lock()
	delete(s.agents, sess.ID)
	s.mu.Unlock()

	if err := s.store.DeleteSession(sess.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	sessions, err := s.store.ListSessions(project, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sessions == nil {
		sessions = []domain.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs, err := s.store.GetMessages(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// handleSubmit runs the agent loop and streams events as SSE.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty text"})
		return
	}

	ag, err := s.getOrCreateAgent(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}
	// Events can be emitted from parallel tool goroutines. Serialize SSE writes
	// to avoid interleaved chunked responses (which appear as malformed chunked encoding).
	var sseMu sync.Mutex
	sendSSE := func(event string, data any) {
		sseMu.Lock()
		defer sseMu.Unlock()
		writeSSE(w, flusher, event, data)
	}

	ag.Submit(req.Text, func(evt agent.Event) {
		switch evt.Kind {
		case agent.EventDelta:
			sendSSE("delta", map[string]string{"text": evt.DeltaText})

		case agent.EventToolStart:
			sendSSE("tool_start", map[string]string{
				"tool_use_id": evt.ToolUseID,
				"tool_name":   evt.ToolName,
			})

		case agent.EventToolDone:
			sendSSE("tool_done", map[string]any{
				"tool_use_id": evt.ToolUseID,
				"tool_name":   evt.ToolName,
				"result":      evt.ToolResult,
				"is_error":    evt.ToolIsError,
			})

		case agent.EventStreamDone:
			sendSSE("stream_done", map[string]any{
				"input_tokens":                evt.InputTokens,
				"output_tokens":               evt.OutputTokens,
				"cache_creation_input_tokens": evt.CacheCreationInputTokens,
				"cache_read_input_tokens":     evt.CacheReadInputTokens,
				"stop_reason":                 evt.StopReason,
			})

		case agent.EventAskUser:
			askID := domain.NewUUID()
			s.mu.Lock()
			s.askChans[askID] = evt.AskResponse
			s.mu.Unlock()

			sendSSE("ask_user", map[string]string{
				"ask_id": askID,
				"prompt": evt.AskPrompt,
			})

		case agent.EventRetrying:
			sendSSE("retrying", map[string]any{
				"attempt": evt.RetryAttempt,
				"wait_ms": evt.RetryAfter.Milliseconds(),
				"message": evt.RetryMessage,
			})

		case agent.EventTurnDone:
			sendSSE("turn_done", map[string]string{
				"stop_reason": evt.StopReason,
			})

		case agent.EventError:
			errMsg := "unknown error"
			if evt.Err != nil {
				errMsg = evt.Err.Error()
			}
			sendSSE("error", map[string]string{"error": errMsg})

		case agent.EventCompacted:
			sendSSE("compacted", map[string]string{})

		case agent.EventTitled:
			sendSSE("titled", map[string]string{
				"title": evt.NewTitle,
				"tags":  evt.NewTags,
			})
		}
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	s.mu.Lock()
	ag, ok := s.agents[sessionID]
	s.mu.Unlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active agent for session"})
		return
	}
	ag.Cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleAskResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AskID  string `json:"ask_id"`
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	s.mu.Lock()
	ch, ok := s.askChans[req.AskID]
	if ok {
		delete(s.askChans, req.AskID)
	}
	s.mu.Unlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown ask_id"})
		return
	}

	ch <- req.Answer
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req struct {
		Label   string `json:"label"`
		ModelID string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Resolve provider from the selected model spec so daemon and TUI stay aligned.
	spec := strings.TrimSpace(req.Label)
	if spec == "" {
		spec = strings.TrimSpace(req.ModelID)
	}
	currentProvider := ""
	if s.provider != nil {
		currentProvider = s.provider.Name()
	}
	newProviderName, _ := provider.ResolveProviderAndModel(spec, currentProvider)
	newProvider, provErr := provider.GetProvider(newProviderName)
	if provErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": provErr.Error()})
		return
	}

	s.mu.Lock()
	newAPIKey := s.apiKey
	if key, err := config.LoadProviderAPIKey(*s.prefs, newProviderName); err == nil {
		newAPIKey = key
	}
	s.modelID = req.ModelID
	s.modelLabel = req.Label
	s.provider = newProvider
	s.apiKey = newAPIKey
	if s.prefs != nil {
		s.prefs.Model = req.Label
		s.prefs.Provider = newProviderName
	}
	ag, ok := s.agents[sessionID]
	s.mu.Unlock()

	if ok {
		ag.SetProvider(newProvider, newAPIKey)
		ag.SetModel(req.Label, req.ModelID)
	} else {
		if err := s.store.UpdateSessionModel(sessionID, req.ModelID); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: update session model: %v\n", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"label":    req.Label,
		"model_id": req.ModelID,
	})
}

func (s *Server) handleSetTitle(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := s.store.UpdateSessionTitle(sessionID, req.Title); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"title":  req.Title,
	})
}

func (s *Server) handleBranch(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req struct {
		AtSequence int `json:"at_sequence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	newSess, err := s.store.BranchSession(sessionID, req.AtSequence)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, newSess)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	prefs := *s.prefs
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	s.mu.Lock()
	if err := s.prefs.Set(req.Key, req.Value); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := config.SavePreferences(*s.prefs); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// If an API key was changed, re-resolve and update the server's active key
	if strings.HasSuffix(req.Key, ".api_key") {
		provName := strings.TrimSuffix(req.Key, ".api_key")
		if key, err := config.LoadProviderAPIKey(*s.prefs, provName); err == nil {
			s.apiKey = key
		}
	}
	if req.Key == "ollama.url" {
		provider.SetOllamaBaseURL(req.Value)
	}
	if req.Key == "brave.api_key" {
		for _, ag := range s.agents {
			ag.SetBraveAPIKey(req.Value)
		}
	}
	if strings.HasPrefix(req.Key, "x.") {
		for _, ag := range s.agents {
			ag.SetXOAuth(
				s.prefs.XClientID,
				s.prefs.XClientSecret,
				s.prefs.XAccessToken,
				s.prefs.XRefreshToken,
				s.prefs.XTokenExpiry,
				func(accessToken, refreshToken, tokenExpiry string) error {
					s.mu.Lock()
					defer s.mu.Unlock()
					s.prefs.XAccessToken = accessToken
					s.prefs.XRefreshToken = refreshToken
					s.prefs.XTokenExpiry = tokenExpiry
					return config.SavePreferences(*s.prefs)
				},
			)
		}
	}
	if req.Key == "tools.disabled" {
		disabled := s.prefs.DisabledToolsSet()
		for _, ag := range s.agents {
			ag.SetDisabledTools(disabled)
		}
	}
	display := s.prefs.Get(req.Key)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": fmt.Sprintf("Set %s = %s", req.Key, display),
	})
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mgr := s.mcpManager
	s.mu.Unlock()
	if mgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"tools":    []string{},
			"statuses": map[string]string{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":    mgr.ToolNames(),
		"statuses": mgr.ServerStatuses(),
	})
}

// ---------------------------------------------------------------------------
// Agent lifecycle
// ---------------------------------------------------------------------------

func (s *Server) getOrCreateAgent(sessionID string) (*agent.Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ag, ok := s.agents[sessionID]; ok {
		// Sync provider if the agent was created before a model was configured.
		if !ag.HasProvider() && s.provider != nil {
			ag.SetProvider(s.provider, s.apiKey)
			ag.SetModel(s.modelLabel, s.modelID)
		}
		return ag, nil
	}

	sess, err := s.store.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("loading session: %w", err)
	}

	if s.newAgent == nil {
		return nil, fmt.Errorf("no agent factory configured")
	}

	ag := s.newAgent(s.apiKey, s.modelID, s.modelLabel, s.store, sess, s.provider)

	// Try to resume messages from DB
	if msgs, err := s.store.GetMessages(sessionID); err == nil && len(msgs) > 0 {
		if err := ag.Resume(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: resume agent %s: %v\n", sessionID, err)
		}
	}

	s.configureAgent(ag)

	s.agents[sessionID] = ag
	return ag, nil
}

// configureAgent sets up credentials, disabled tools, MCP, git, and memory
// on an agent. Must be called with s.mu held.
func (s *Server) configureAgent(ag *agent.Service) {
	if s.prefs != nil && s.prefs.BraveAPIKey != "" {
		ag.SetBraveAPIKey(s.prefs.BraveAPIKey)
	}
	if s.prefs != nil {
		ag.SetXOAuth(
			s.prefs.XClientID,
			s.prefs.XClientSecret,
			s.prefs.XAccessToken,
			s.prefs.XRefreshToken,
			s.prefs.XTokenExpiry,
			func(accessToken, refreshToken, tokenExpiry string) error {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.prefs.XAccessToken = accessToken
				s.prefs.XRefreshToken = refreshToken
				s.prefs.XTokenExpiry = tokenExpiry
				return config.SavePreferences(*s.prefs)
			},
		)
	}
	if s.prefs != nil {
		ag.SetDisabledTools(s.prefs.DisabledToolsSet())
	}
	if s.mcpManager != nil {
		ag.SetMCPManager(s.mcpManager)
	}

	// Detect git repo
	if s.detectGitRepo != nil {
		if root, ok := s.detectGitRepo(); ok {
			ag.SetGitAvailable(true, root)
		}
	}

	// Set up project memory
	cwd, _ := tools.Getwd()
	if cwd != "" {
		ag.SetMemory(tools.NewProjectMemory(cwd))
	}
}

// ---------------------------------------------------------------------------
// Scheduled agent task execution
// ---------------------------------------------------------------------------

// executeScheduledAgentTask spawns a full agent loop for a scheduled agent task.
func (s *Server) executeScheduledAgentTask(call tools.ScheduledToolCall) (string, bool, error) {
	promptRaw, ok := call.ToolInput["prompt"]
	if !ok {
		return "", true, fmt.Errorf("agent task missing prompt")
	}
	prompt, _ := promptRaw.(string)
	if strings.TrimSpace(prompt) == "" {
		return "", true, fmt.Errorf("agent task has empty prompt")
	}

	// Create ephemeral session for this scheduled task.
	sess, err := s.store.CreateSession("__scheduled_task__", s.modelID)
	if err != nil {
		return "", true, fmt.Errorf("creating scheduled task session: %w", err)
	}

	if s.newAgent == nil {
		return "", true, fmt.Errorf("no agent factory configured")
	}

	s.mu.Lock()
	ag := s.newAgent(s.apiKey, s.modelID, s.modelLabel, s.store, sess, s.provider)
	s.configureAgent(ag)
	s.mu.Unlock()

	// Disable ask_user in headless mode — no one to answer.
	ag.SetDisabledTools(map[string]bool{"ask_user": true})

	var result strings.Builder
	const maxResultSize = 50 * 1024

	ag.Submit(prompt, func(evt agent.Event) {
		switch evt.Kind {
		case agent.EventDelta:
			if result.Len() < maxResultSize {
				result.WriteString(evt.DeltaText)
			}
		case agent.EventError:
			if evt.Err != nil {
				result.WriteString("\nError: " + evt.Err.Error())
			}
		}
	})

	out := result.String()
	if len(out) > maxResultSize {
		out = out[:maxResultSize] + "\n... (truncated at 50KB)"
	}

	return out, false, nil
}

// ---------------------------------------------------------------------------
// SSE + JSON helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: write json response: %v\n", err)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	flusher.Flush()
}

// sseKeepAlive sends periodic SSE comments to keep the connection alive.
// Not currently used but available for future long-polling scenarios.
func sseKeepAlive(w http.ResponseWriter, flusher http.Flusher, done <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
