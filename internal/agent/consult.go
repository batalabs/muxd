package agent

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

const consultSystemPrompt = "You are providing a second opinion on a coding problem. Be concise and direct. Agree or disagree with the approach and explain why. Keep your response under 300 words."

// Consult sends summary to the configured consult model and returns the
// response text. Returns an error if no consult model is configured.
func (a *Service) Consult(summary string) (string, error) {
	a.mu.Lock()
	modelConsult := a.modelConsult
	primaryProvider := a.prov
	primaryAPIKey := a.apiKey
	a.mu.Unlock()

	if modelConsult == "" {
		return "", fmt.Errorf("no consult model configured")
	}

	providerName, modelID := provider.ResolveProviderAndModel(modelConsult, "")

	a.mu.Lock()
	prefs := a.prefs
	a.mu.Unlock()

	apiKey, _ := config.LoadProviderAPIKey(prefs, providerName)

	// Fall back to the primary API key when the consult model uses the same
	// provider as the primary model and no explicit key was resolved.
	if apiKey == "" && primaryProvider != nil && primaryProvider.Name() == providerName {
		apiKey = primaryAPIKey
	}

	prov, err := provider.GetProvider(providerName)
	if err != nil {
		return "", fmt.Errorf("consult: resolving provider: %w", err)
	}

	return consultWithProvider(prov, apiKey, modelID, summary)
}

// consultWithProvider is the testable core of Consult. It sends a single-turn
// request with consultSystemPrompt as the system prompt and summary as the
// user message. No tools are included. Returns the collected response text.
func consultWithProvider(prov provider.Provider, apiKey, modelID, summary string) (string, error) {
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: summary},
	}

	blocks, _, _, err := prov.StreamMessage(apiKey, modelID, msgs, nil, consultSystemPrompt, nil)
	if err != nil {
		return "", fmt.Errorf("consult: stream: %w", err)
	}

	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), nil
}
