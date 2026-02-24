package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/batalabs/muxd/internal/domain"
)

var zaiAPIBaseURL = "https://api.z.ai/api/paas/v4"

// setZAIBaseURL overrides the base URL (used in tests).
func setZAIBaseURL(url string) { zaiAPIBaseURL = url }

// ZAIProvider implements Provider for Z.AI's chat API.
// It uses OpenAI-compatible request/stream formats.
type ZAIProvider struct{}

// Name returns "zai".
func (p *ZAIProvider) Name() string { return "zai" }

// FetchModels retrieves the list of models from Z.AI.
func (p *ZAIProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	httpReq, err := http.NewRequest(http.MethodGet, zaiAPIBaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var listResp struct {
		Data []domain.APIModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return listResp.Data, nil
}

// StreamMessage sends a streaming chat completion request to Z.AI.
func (p *ZAIProvider) StreamMessage(
	apiKey, modelID string,
	history []domain.TranscriptMessage,
	tools []ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, Usage, error) {
	msgs := buildOpenAIMessages(history, system)
	streamOpts := &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}

	reqBody := openaiRequest{
		Model:         modelID,
		Messages:      msgs,
		Stream:        true,
		Tools:         toOpenAITools(tools),
		StreamOptions: streamOpts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, zaiAPIBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept-Encoding", "identity")

	resp, err := streamHTTPClient.Do(httpReq)
	if err != nil {
		return nil, "", Usage{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		errType := ""
		errMessage := fmt.Sprintf("HTTP %d", resp.StatusCode)
		var errResp struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &errResp) == nil && errResp.Error != nil {
			errType = errResp.Error.Type
			errMessage = errResp.Error.Message
		}
		return nil, "", Usage{}, NewAPIError(resp.StatusCode, errType, errMessage, resp.Header)
	}

	return parseOpenAISSE(resp.Body, onDelta)
}
