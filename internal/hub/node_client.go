package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

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
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Register registers this node with the hub. Returns the assigned node ID.
func (c *NodeClient) Register(name, host string, port int, version string) (string, error) {
	body, err := json.Marshal(registerRequest{
		Name:    name,
		Host:    host,
		Port:    port,
		Token:   c.nodeToken,
		Version: version,
	})
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

// Heartbeat sends a liveness signal to the hub.
func (c *NodeClient) Heartbeat(nodeID string) error {
	req, err := http.NewRequest("POST", c.baseURL+"/api/hub/nodes/"+nodeID+"/heartbeat", nil)
	if err != nil {
		return fmt.Errorf("creating heartbeat request: %w", err)
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
