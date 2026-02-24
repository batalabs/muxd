package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
)

// SSEEvent represents a parsed server-sent event from the daemon.
type SSEEvent struct {
	Type                     string // "delta", "tool_start", "tool_done", "stream_done", "ask_user", "turn_done", "error", "compacted", "titled", "retrying"
	DeltaText                string
	ToolUseID                string
	ToolName                 string
	ToolResult               string
	ToolIsError              bool
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	StopReason               string
	AskID                    string
	AskPrompt                string
	ErrorMsg                 string
	Title                    string
	Tags                     string
	RetryAttempt             int
	RetryWaitMs              int
	RetryMessage             string
}

// DaemonClient is the HTTP client used by the TUI to communicate with the daemon server.
type DaemonClient struct {
	baseURL    string
	httpClient *http.Client
	authToken  string
}

// NewDaemonClient creates a new client for the daemon at the given port.
func NewDaemonClient(port int) *DaemonClient {
	return &DaemonClient{
		baseURL:    fmt.Sprintf("http://localhost:%d", port),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// SetAuthToken sets the daemon bearer token used on protected endpoints.
func (c *DaemonClient) SetAuthToken(token string) {
	c.authToken = strings.TrimSpace(token)
}

func (c *DaemonClient) do(req *http.Request) (*http.Response, error) {
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	return c.httpClient.Do(req)
}

// Health checks if the daemon is responding.
func (c *DaemonClient) Health() error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(c.baseURL + "/api/health")
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: status %d", resp.StatusCode)
	}
	return nil
}

// CreateSession creates a new session on the daemon.
func (c *DaemonClient) CreateSession(projectPath, modelID string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"project_path": projectPath,
		"model_id":     modelID,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		SessionID string `json:"session_id"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("creating session: %s", result.Error)
	}
	return result.SessionID, nil
}

// GetSession retrieves session metadata.
func (c *DaemonClient) GetSession(sessionID string) (*domain.Session, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/sessions/"+sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	var sess domain.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return nil, fmt.Errorf("parsing session: %w", err)
	}
	return &sess, nil
}

// ListSessions lists sessions for the given project path.
func (c *DaemonClient) ListSessions(projectPath string, limit int) ([]domain.Session, error) {
	url := fmt.Sprintf("%s/api/sessions?project=%s&limit=%d", c.baseURL, projectPath, limit)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	defer resp.Body.Close()

	var sessions []domain.Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("parsing sessions: %w", err)
	}
	return sessions, nil
}

// GetMessages retrieves the message history for a session.
func (c *DaemonClient) GetMessages(sessionID string) ([]domain.TranscriptMessage, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/sessions/"+sessionID+"/messages", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}
	defer resp.Body.Close()

	var msgs []domain.TranscriptMessage
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, fmt.Errorf("parsing messages: %w", err)
	}
	return msgs, nil
}

// Submit sends a user message and streams SSE events back via the callback.
// This call blocks until the turn is complete.
func (c *DaemonClient) Submit(sessionID, text string, onEvent func(SSEEvent)) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequest("POST", c.baseURL+"/api/sessions/"+sessionID+"/submit", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// No timeout for long-running submit
	client := &http.Client{}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("submitting: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("submit failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	return parseSSEStream(resp.Body, onEvent)
}

// Cancel cancels the running agent loop for a session.
func (c *DaemonClient) Cancel(sessionID string) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/sessions/"+sessionID+"/cancel", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("cancelling: %w", err)
	}
	resp.Body.Close()
	return nil
}

// SendAskResponse sends the user's answer to a pending ask_user question.
func (c *DaemonClient) SendAskResponse(sessionID, askID, answer string) error {
	body, _ := json.Marshal(map[string]string{
		"ask_id": askID,
		"answer": answer,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/sessions/"+sessionID+"/ask-response", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("sending ask response: %w", err)
	}
	resp.Body.Close()
	return nil
}

// SetModel changes the model for a session.
func (c *DaemonClient) SetModel(sessionID, label, modelID string) error {
	body, _ := json.Marshal(map[string]string{
		"label":    label,
		"model_id": modelID,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/sessions/"+sessionID+"/model", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("setting model: %w", err)
	}
	resp.Body.Close()
	return nil
}

// GetConfig retrieves the current preferences from the daemon.
func (c *DaemonClient) GetConfig() (*config.Preferences, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/config", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting config: %w", err)
	}
	defer resp.Body.Close()

	var prefs config.Preferences
	if err := json.NewDecoder(resp.Body).Decode(&prefs); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &prefs, nil
}

// SetConfig updates a preference key on the daemon.
func (c *DaemonClient) SetConfig(key, value string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"key":   key,
		"value": value,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/config", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("setting config: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.Message, nil
}

// MCPToolsResponse holds the response from the /api/mcp/tools endpoint.
type MCPToolsResponse struct {
	Tools    []string          `json:"tools"`
	Statuses map[string]string `json:"statuses"`
}

// GetMCPTools retrieves the list of MCP tool names and server statuses.
func (c *DaemonClient) GetMCPTools() (*MCPToolsResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/mcp/tools", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting MCP tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getting MCP tools: HTTP %d", resp.StatusCode)
	}

	var result MCPToolsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing MCP tools: %w", err)
	}
	return &result, nil
}

// WaitReady polls Health() until the daemon is responsive or the timeout is reached.
func (c *DaemonClient) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := c.Health(); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon not ready after %v", timeout)
}

// BranchSession creates a new session forked from the given session.
func (c *DaemonClient) BranchSession(sessionID string, atSequence int) (*domain.Session, error) {
	body, _ := json.Marshal(map[string]int{"at_sequence": atSequence})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/sessions/"+sessionID+"/branch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("branching session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			// best-effort error body parsing; fallback message used regardless
		}
		return nil, fmt.Errorf("branching session: %s", errResp.Error)
	}

	var sess domain.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return nil, fmt.Errorf("parsing branch response: %w", err)
	}
	return &sess, nil
}

// SetBaseURL overrides the base URL (useful for testing).
func (c *DaemonClient) SetBaseURL(url string) {
	c.baseURL = url
}

// ---------------------------------------------------------------------------
// SSE parsing
// ---------------------------------------------------------------------------

func parseSSEStream(body io.Reader, onEvent func(SSEEvent)) error {
	scanner := bufio.NewScanner(body)
	// Increase buffer size for large tool results
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	sawStreamDone := false
	sawTurnDone := false
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			evt := ParseSSEEvent(eventType, data)
			if evt.Type != "" {
				if evt.Type == "stream_done" {
					sawStreamDone = true
				}
				if evt.Type == "turn_done" {
					sawTurnDone = true
				}
				onEvent(evt)
			}
			eventType = ""
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		// If we already observed completion, tolerate common unclean stream tails.
		if (sawStreamDone || sawTurnDone) && isRecoverableSSEStreamErr(err) {
			return nil
		}
		return err
	}
	return nil
}

func isRecoverableSSEStreamErr(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected EOF") ||
		strings.Contains(msg, "chunked line ends with bare LF") ||
		strings.Contains(msg, "invalid byte in chunk length")
}

// ParseSSEEvent parses a single SSE event from its type and JSON data.
func ParseSSEEvent(eventType, data string) SSEEvent {
	var raw map[string]any
	if json.Unmarshal([]byte(data), &raw) != nil {
		return SSEEvent{}
	}

	evt := SSEEvent{Type: eventType}

	switch eventType {
	case "delta":
		evt.DeltaText, _ = raw["text"].(string)

	case "tool_start":
		evt.ToolUseID, _ = raw["tool_use_id"].(string)
		evt.ToolName, _ = raw["tool_name"].(string)

	case "tool_done":
		evt.ToolUseID, _ = raw["tool_use_id"].(string)
		evt.ToolName, _ = raw["tool_name"].(string)
		evt.ToolResult, _ = raw["result"].(string)
		evt.ToolIsError, _ = raw["is_error"].(bool)

	case "stream_done":
		if v, ok := raw["input_tokens"].(float64); ok {
			evt.InputTokens = int(v)
		}
		if v, ok := raw["output_tokens"].(float64); ok {
			evt.OutputTokens = int(v)
		}
		if v, ok := raw["cache_creation_input_tokens"].(float64); ok {
			evt.CacheCreationInputTokens = int(v)
		}
		if v, ok := raw["cache_read_input_tokens"].(float64); ok {
			evt.CacheReadInputTokens = int(v)
		}
		evt.StopReason, _ = raw["stop_reason"].(string)

	case "ask_user":
		evt.AskID, _ = raw["ask_id"].(string)
		evt.AskPrompt, _ = raw["prompt"].(string)

	case "turn_done":
		evt.StopReason, _ = raw["stop_reason"].(string)

	case "error":
		evt.ErrorMsg, _ = raw["error"].(string)

	case "compacted":
		// No fields

	case "titled":
		evt.Title, _ = raw["title"].(string)
		evt.Tags, _ = raw["tags"].(string)

	case "retrying":
		if v, ok := raw["attempt"].(float64); ok {
			evt.RetryAttempt = int(v)
		}
		if v, ok := raw["wait_ms"].(float64); ok {
			evt.RetryWaitMs = int(v)
		}
		evt.RetryMessage, _ = raw["message"].(string)

	default:
		return SSEEvent{}
	}

	return evt
}
