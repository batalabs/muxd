package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HubClient is used by the TUI to communicate with a hub for node listing.
// Once a node is selected, all session/agent traffic goes through the existing
// DaemonClient with a proxy-prefixed baseURL.
type HubClient struct {
	baseURL   string
	authToken string
	client    *http.Client
}

// NewHubClient creates a client for TUI-to-hub communication.
func NewHubClient(baseURL, token string) *HubClient {
	return &HubClient{
		baseURL:   baseURL,
		authToken: token,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ListNodes retrieves the list of registered nodes from the hub.
func (c *HubClient) ListNodes() ([]*Node, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/hub/nodes", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing nodes: HTTP %d", resp.StatusCode)
	}

	var nodes []*Node
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		return nil, fmt.Errorf("parsing nodes: %w", err)
	}
	return nodes, nil
}
