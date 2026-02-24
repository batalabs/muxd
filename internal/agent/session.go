package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
)

// generateAndSetTitle generates a title and tags for the session using the LLM,
// falling back to a truncated user message on failure.
func (a *Service) generateAndSetTitle(asstText string, onEvent EventFunc) {
	a.mu.Lock()
	var userText string
	for _, tmsg := range a.messages {
		if tmsg.Role == "user" {
			userText = tmsg.TextContent()
			break
		}
	}
	a.mu.Unlock()

	if userText == "" {
		return
	}

	// Use a simple truncation instead of an LLM call to avoid burning
	// an extra API request (which causes immediate rate limiting on Opus).
	title := userText
	if len(title) > 50 {
		title = title[:50] + "..."
	}
	title = strings.Join(strings.Fields(title), " ")

	a.mu.Lock()
	a.session.Title = title
	a.mu.Unlock()

	if err := a.store.UpdateSessionTitle(a.session.ID, title); err != nil {
		fmt.Fprintf(os.Stderr, "agent: update session title: %v\n", err)
	}

	onEvent(Event{Kind: EventTitled, NewTitle: title})
}

// Cancel signals the running Submit loop to stop at the next safe point.
func (a *Service) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelled = true
}

// Resume loads messages from the database for the current session.
// If a compaction record exists, it loads the summary as synthetic messages
// plus the tail messages after the cutoff point.
func (a *Service) Resume() error {
	if a.store == nil {
		return fmt.Errorf("no store available")
	}
	if a.session == nil {
		return fmt.Errorf("no session available")
	}

	// Check for persisted compaction first.
	summary, cutoff, compErr := a.store.LatestCompaction(a.session.ID)
	if compErr == nil && cutoff > 0 {
		// Load compacted state: summary + tail messages.
		tail, err := a.store.GetMessagesAfterSequence(a.session.ID, cutoff)
		if err != nil {
			return fmt.Errorf("loading tail messages: %w", err)
		}
		// Prefix with marker if summary doesn't already have one.
		content := summary
		if !strings.HasPrefix(content, "[") {
			content = "[Previous conversation summary]\n\n" + content
		}
		var msgs []domain.TranscriptMessage
		msgs = append(msgs,
			domain.TranscriptMessage{Role: "user", Content: content},
			domain.TranscriptMessage{Role: "assistant", Content: "Understood. I'll continue with the context available."},
		)
		msgs = append(msgs, tail...)
		a.mu.Lock()
		a.messages = msgs
		a.titled = true
		a.mu.Unlock()
		return nil
	}

	// No compaction — load all messages.
	msgs, err := a.store.GetMessages(a.session.ID)
	if err != nil {
		return fmt.Errorf("loading messages: %w", err)
	}
	a.mu.Lock()
	a.messages = msgs
	a.titled = len(msgs) > 0
	a.mu.Unlock()
	return nil
}

// SetModel changes the active model.
func (a *Service) SetModel(label, id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelLabel = label
	a.modelID = id
	if a.store != nil && a.session != nil {
		if err := a.store.UpdateSessionModel(a.session.ID, id); err != nil {
			fmt.Fprintf(os.Stderr, "agent: update session model: %v\n", err)
		}
	}
}

// SetProvider changes the active provider and API key.
func (a *Service) SetProvider(prov provider.Provider, apiKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prov = prov
	a.apiKey = apiKey
}

// HasProvider reports whether a provider is configured on this agent.
func (a *Service) HasProvider() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prov != nil
}

// Messages returns a copy of the current message history.
func (a *Service) Messages() []domain.TranscriptMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]domain.TranscriptMessage, len(a.messages))
	copy(out, a.messages)
	return out
}

// NewSession creates a new session and resets conversation state.
func (a *Service) NewSession(projectPath string) error {
	if a.store == nil {
		return fmt.Errorf("no store available")
	}
	sess, err := a.store.CreateSession(projectPath, a.modelID)
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	a.mu.Lock()
	a.session = sess
	a.messages = nil
	a.inputTokens = 0
	a.outputTokens = 0
	a.lastInputTokens = 0
	a.titled = false
	a.agentLoopCount = 0
	a.checkpoints = nil
	a.redoStack = nil
	a.mu.Unlock()
	return nil
}

// Session returns the current session.
func (a *Service) Session() *domain.Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session
}

// SetBraveAPIKey sets the Brave Search API key from preferences.
func (a *Service) SetBraveAPIKey(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.braveAPIKey = key
}

// SetXOAuth configures X OAuth runtime credentials for this agent.
func (a *Service) SetXOAuth(clientID, clientSecret, accessToken, refreshToken, tokenExpiry string, saver func(accessToken, refreshToken, tokenExpiry string) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.xClientID = clientID
	a.xClientSecret = clientSecret
	a.xAccessToken = accessToken
	a.xRefreshToken = refreshToken
	a.xTokenExpiry = tokenExpiry
	a.xTokenSaver = saver
}

// SetMCPManager sets the MCP server manager for tool routing.
func (a *Service) SetMCPManager(m *mcp.Manager) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mcpManager = m
}

// SetDisabledTools replaces the user-disabled tools set.
func (a *Service) SetDisabledTools(disabled map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.disabledTools = make(map[string]bool, len(disabled))
	for k, v := range disabled {
		if v {
			a.disabledTools[k] = true
		}
	}
}

// SetGitAvailable configures git checkpoint support.
func (a *Service) SetGitAvailable(available bool, repoRoot string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gitAvailable = available
	a.gitRepoRoot = repoRoot
}

// IsRunning reports whether a Submit is currently in progress.
func (a *Service) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// SpawnSubAgent creates a sub-agent Service, runs a prompt to completion, and
// returns the concatenated text output. The sub-agent has no store (no
// persistence), no git checkpoints, and cannot use the task tool.
func (a *Service) SpawnSubAgent(description, prompt string) (string, error) {
	a.mu.Lock()
	disabled := make(map[string]bool, len(a.disabledTools))
	for k, v := range a.disabledTools {
		disabled[k] = v
	}
	mcpMgr := a.mcpManager
	a.mu.Unlock()

	sub := &Service{
		apiKey:        a.apiKey,
		modelID:       a.modelID,
		prov:          a.prov,
		isSubAgent:    true,
		Cwd:           a.Cwd,
		disabledTools: disabled,
		mcpManager:    mcpMgr,
	}

	var output strings.Builder
	var subErr error

	sub.Submit(prompt, func(evt Event) {
		switch evt.Kind {
		case EventDelta:
			_, _ = output.WriteString(evt.DeltaText) // strings.Builder.Write never fails
		case EventError:
			subErr = evt.Err
		}
	})

	if subErr != nil {
		return "", subErr
	}

	result := output.String()
	const maxOutput = 50 * 1024
	if len(result) > maxOutput {
		result = result[:maxOutput] + "\n... (sub-agent output truncated at 50KB)"
	}

	return result, nil
}
