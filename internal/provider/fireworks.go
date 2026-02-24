package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/batalabs/muxd/internal/domain"
)

var fireworksAPIBaseURL = "https://api.fireworks.ai/inference/v1"

// setFireworksBaseURL overrides the base URL (used in tests).
func setFireworksBaseURL(url string) { fireworksAPIBaseURL = url }

// FireworksProvider implements Provider for Fireworks AI's chat API.
// It uses OpenAI-compatible request/stream formats.
type FireworksProvider struct{}

// Name returns "fireworks".
func (p *FireworksProvider) Name() string { return "fireworks" }

// FetchModels retrieves the list of models from Fireworks AI.
func (p *FireworksProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	httpReq, err := http.NewRequest(http.MethodGet, fireworksAPIBaseURL+"/models", nil)
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

// StreamMessage sends a streaming chat completion request to Fireworks AI.
func (p *FireworksProvider) StreamMessage(
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

	httpReq, err := http.NewRequest(http.MethodPost, fireworksAPIBaseURL+"/chat/completions", bytes.NewReader(body))
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
		errMessage := string(raw)
		if errMessage == "" {
			errMessage = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
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
