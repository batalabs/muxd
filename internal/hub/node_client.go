package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/daemon"
)

const nodeClientTimeout = 10 * time.Second

// NodeClient is used by muxd daemon instances to communicate with the hub.
type NodeClient struct {
	baseURL   string
	hubToken  string
	nodeToken string
	client    *http.Client
}

// NewNodeClient creates a client for node-to-hub communication.
func NewNodeClient(hubURL, hubToken, nodeToken string) *NodeClient {
	return &NodeClient{
		baseURL:   hubURL,
		hubToken:  hubToken,
		nodeToken: nodeToken,
		client:    &http.Client{Timeout: nodeClientTimeout},
	}
}

// NodeInfo holds runtime capabilities sent during registration and heartbeats.
type NodeInfo struct {
	Platform string   `json:"platform,omitempty"`
	Arch     string   `json:"arch,omitempty"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	MCPTools []string `json:"mcp_tools,omitempty"`
}

// Register registers this node with the hub. Returns the assigned node ID.
func (c *NodeClient) Register(name, host string, port int, version string, info ...NodeInfo) (string, error) {
	regReq := registerRequest{
		Name:    name,
		Host:    host,
		Port:    port,
		Token:   c.nodeToken,
		Version: version,
	}
	if len(info) > 0 {
		regReq.Platform = info[0].Platform
		regReq.Arch = info[0].Arch
		regReq.Provider = info[0].Provider
		regReq.Model = info[0].Model
		regReq.Tools = info[0].Tools
		regReq.MCPTools = info[0].MCPTools
	}
	body, err := json.Marshal(regReq)
	if err != nil {
		return "", fmt.Errorf("marshaling register request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/api/hub/nodes/register", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registering with hub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return "", fmt.Errorf("hub register failed (%d): %s", resp.StatusCode, errResp["error"])
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding register response: %w", err)
	}
	return result.ID, nil
}

// NodeListEntry is a node returned by the hub's list-nodes API.
type NodeListEntry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Status   string   `json:"status"`
	Platform string   `json:"platform"`
	Arch     string   `json:"arch"`
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Tools    []string `json:"tools"`
	MCPTools []string `json:"mcp_tools"`
}

// ListNodes fetches the list of all nodes registered with the hub.
func (c *NodeClient) ListNodes() ([]NodeListEntry, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/hub/nodes", nil)
	if err != nil {
		return nil, fmt.Errorf("creating list nodes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing hub nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub list nodes failed: %d", resp.StatusCode)
	}

	var nodes []NodeListEntry
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		return nil, fmt.Errorf("decoding hub nodes: %w", err)
	}
	return nodes, nil
}

// Deregister removes this node from the hub.
func (c *NodeClient) Deregister(nodeID string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/api/hub/nodes/"+nodeID, nil)
	if err != nil {
		return fmt.Errorf("creating deregister request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("deregistering from hub: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub deregister failed: %d", resp.StatusCode)
	}
	return nil
}

// FetchMemory retrieves shared memory facts from the hub.
func (c *NodeClient) FetchMemory() (map[string]string, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/hub/memory", nil)
	if err != nil {
		return nil, fmt.Errorf("creating fetch memory request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching hub memory: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Hub doesn't support memory yet -not an error.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub memory fetch failed: %d", resp.StatusCode)
	}

	var facts map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&facts); err != nil {
		return nil, fmt.Errorf("decoding hub memory: %w", err)
	}
	return facts, nil
}

// PushMemory sends memory facts to the hub for shared storage.
func (c *NodeClient) PushMemory(facts map[string]string) error {
	body, err := json.Marshal(map[string]any{"facts": facts})
	if err != nil {
		return fmt.Errorf("marshaling memory: %w", err)
	}

	req, err := http.NewRequest("PUT", c.baseURL+"/api/hub/memory", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating push memory request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("pushing hub memory: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub memory push failed: %d", resp.StatusCode)
	}
	return nil
}

// Heartbeat sends a liveness signal to the hub, optionally refreshing capabilities.
func (c *NodeClient) Heartbeat(nodeID string, info ...NodeInfo) error {
	var bodyReader *bytes.Reader
	if len(info) > 0 {
		b, _ := json.Marshal(info[0])
		bodyReader = bytes.NewReader(b)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest("POST", c.baseURL+"/api/hub/nodes/"+nodeID+"/heartbeat", bodyReader)
		if err != nil {
			return fmt.Errorf("creating heartbeat request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest("POST", c.baseURL+"/api/hub/nodes/"+nodeID+"/heartbeat", nil)
		if err != nil {
			return fmt.Errorf("creating heartbeat request: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending heartbeat: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub heartbeat failed: %d", resp.StatusCode)
	}
	return nil
}

// Dispatch sends a task to a remote node via the hub proxy. It creates a
// session on the target node, submits the prompt, reads the SSE stream to
// collect the agent's text output, and returns it.
func (c *NodeClient) Dispatch(nodeIDOrName, prompt string) (string, error) {
	// 1. Resolve node name → node ID.
	nodeID, err := c.resolveNodeID(nodeIDOrName)
	if err != nil {
		return "", err
	}

	// 2. Create a session on the target node via hub proxy.
	sessionID, err := c.proxyCreateSession(nodeID)
	if err != nil {
		return "", fmt.Errorf("creating remote session: %w", err)
	}

	// 3. Submit the prompt and collect SSE output.
	result, err := c.proxySubmit(nodeID, sessionID, prompt)
	if err != nil {
		return "", fmt.Errorf("remote submit: %w", err)
	}

	return result, nil
}

// resolveNodeID looks up a node by name or ID from the hub's node list.
func (c *NodeClient) resolveNodeID(nameOrID string) (string, error) {
	nodes, err := c.ListNodes()
	if err != nil {
		return "", fmt.Errorf("listing nodes: %w", err)
	}

	lower := strings.ToLower(nameOrID)
	for _, n := range nodes {
		if n.ID == nameOrID || strings.ToLower(n.Name) == lower {
			if n.Status != "online" {
				return "", fmt.Errorf("node %q is %s (not online)", n.Name, n.Status)
			}
			return n.ID, nil
		}
	}
	return "", fmt.Errorf("node %q not found", nameOrID)
}

// proxyCreateSession creates a new session on a remote node via the hub proxy.
func (c *NodeClient) proxyCreateSession(nodeID string) (string, error) {
	body, _ := json.Marshal(map[string]string{"project_path": "__hub_dispatch__"})
	url := fmt.Sprintf("%s/api/hub/proxy/%s/api/sessions", c.baseURL, nodeID)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding session response: %w", err)
	}
	return result.SessionID, nil
}

// proxySubmit submits a prompt to a remote session via the hub proxy and
// collects the agent's text output from the SSE stream.
func (c *NodeClient) proxySubmit(nodeID, sessionID, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]string{"text": prompt})
	url := fmt.Sprintf("%s/api/hub/proxy/%s/api/sessions/%s/submit", c.baseURL, nodeID, sessionID)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.hubToken)

	// No timeout — agent loops can take a long time.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}

	const maxOutput = 50 * 1024
	var output strings.Builder

	err = daemon.ParseSSEStream(resp.Body, func(evt daemon.SSEEvent) {
		switch evt.Type {
		case "delta":
			if output.Len() < maxOutput {
				output.WriteString(evt.DeltaText)
			}
		case "error":
			if evt.ErrorMsg != "" {
				output.WriteString("\nError: " + evt.ErrorMsg)
			}
		}
	})
	if err != nil {
		return "", fmt.Errorf("reading SSE stream: %w", err)
	}

	result := output.String()
	if len(result) > maxOutput {
		result = result[:maxOutput] + "\n... (truncated at 50KB)"
	}

	return result, nil
}
