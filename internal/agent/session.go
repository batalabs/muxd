package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/tools"
)

// generateAndSetTitle generates a title for the session. If model.title is
// configured, it uses a cheap LLM call to produce a concise title. Otherwise
// it falls back to truncating the first user message. Skipped entirely if the
// user has manually renamed the session.
func (a *Service) generateAndSetTitle(asstText string, onEvent EventFunc) {
	a.mu.Lock()
	if a.userRenamed {
		a.mu.Unlock()
		return
	}
	var userText string
	for _, tmsg := range a.messages {
		if tmsg.Role == "user" {
			userText = tmsg.TextContent()
			break
		}
	}
	titleModel := a.modelTitle
	a.mu.Unlock()

	if userText == "" {
		return
	}

	// Default to the main model when model.title is not configured.
	if titleModel == "" {
		titleModel = a.modelID
	}

	onEvent(Event{Kind: EventToolStart, ToolUseID: "internal_title", ToolName: "generate_title"})

	title := a.generateTitle(userText, asstText, titleModel)

	a.mu.Lock()
	a.session.Title = title
	a.mu.Unlock()

	if err := a.store.UpdateSessionTitle(a.session.ID, title); err != nil {
		fmt.Fprintf(os.Stderr, "agent: update session title: %v\n", err)
	}

	onEvent(Event{Kind: EventToolDone, ToolUseID: "internal_title", ToolName: "generate_title", ToolResult: title + " (model: " + titleModel + ")"})
	onEvent(Event{Kind: EventTitled, NewTitle: title, ModelUsed: titleModel})
}

// generateTitle produces a session title. When titleModel is set and a
// provider is available, it asks the LLM for a short title. Otherwise it
// truncates the user message.
func (a *Service) generateTitle(userText, asstText, titleModel string) string {
	if titleModel != "" && a.prov != nil {
		title := a.llmTitle(userText, asstText, titleModel)
		if title != "" {
			return title
		}
	}
	// Fallback: truncate the first user message.
	title := userText
	if len(title) > 50 {
		title = title[:50] + "..."
	}
	return strings.Join(strings.Fields(title), " ")
}

// llmTitle calls a cheap model to generate a concise session title.
func (a *Service) llmTitle(userText, asstText, titleModel string) string {
	prompt := fmt.Sprintf("Generate a short title (max 50 chars) for this conversation. Return ONLY the title, no quotes or punctuation wrapping.\n\nUser: %s\n\nAssistant: %s", userText, asstText)
	if len(prompt) > 2000 {
		prompt = prompt[:2000]
	}

	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: prompt},
	}
	system := "You generate concise session titles. Return only the title text, nothing else. Maximum 50 characters."

	blocks, _, _, err := a.prov.StreamMessage(a.apiKey, titleModel, msgs, nil, system, nil)
	if err != nil {
		return ""
	}

	var title string
	for _, b := range blocks {
		if b.Type == "text" {
			title += b.Text
		}
	}
	title = strings.TrimSpace(title)
	if len(title) > 60 {
		title = title[:60]
	}
	if title == "" {
		return ""
	}
	return title
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
	a.userRenamed = false
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

// SetTextbeltAPIKey sets the Textbelt SMS API key from preferences.
func (a *Service) SetTextbeltAPIKey(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.textbeltAPIKey = key
}

// SetUserRenamed marks the session as manually renamed by the user,
// preventing auto-title generation from overwriting it.
func (a *Service) SetUserRenamed() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.userRenamed = true
	a.titled = true
}

// SetModelCompact sets the model ID for compaction summaries.
func (a *Service) SetModelCompact(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelCompact = id
}

// SetModelTitle sets the model ID for auto-title generation.
func (a *Service) SetModelTitle(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelTitle = id
}

// SetModelTags sets the model ID for auto-tag generation.
func (a *Service) SetModelTags(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.modelTags = id
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

// UpdateXTokens updates the cached access/refresh tokens and expiry
// without changing client credentials or the saver callback.
// Called after a successful token refresh so subsequent agent loop
// iterations use the new tokens.
func (a *Service) UpdateXTokens(accessToken, refreshToken, tokenExpiry string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.xAccessToken = accessToken
	a.xRefreshToken = refreshToken
	a.xTokenExpiry = tokenExpiry
}

// SetMCPManager sets the MCP server manager for tool routing.
func (a *Service) SetMCPManager(m *mcp.Manager) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mcpManager = m
}

// SetMemory sets the per-project memory store.
func (a *Service) SetMemory(m *tools.ProjectMemory) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.memory = m
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
		memory:        a.memory,
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
