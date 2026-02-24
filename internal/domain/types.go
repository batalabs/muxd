package domain

import (
	"strings"
	"time"
)

// ContentBlock represents a structured content block in a message.
type ContentBlock struct {
	Type       string         `json:"type"`
	Text       string         `json:"text,omitempty"`
	ToolUseID  string         `json:"tool_use_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolInput  map[string]any `json:"tool_input,omitempty"`
	ToolResult string         `json:"tool_result,omitempty"`
	IsError    bool           `json:"is_error,omitempty"`

	// CallerType indicates how a tool_use was invoked.
	// "direct" (standard) or "code_execution_20250825" (PTC).
	CallerType string `json:"caller_type,omitempty"`
	// CallerToolID is the server_tool_use ID that spawned a PTC tool call.
	CallerToolID string `json:"caller_tool_id,omitempty"`
}

// TranscriptMessage is a message with a role and content blocks.
type TranscriptMessage struct {
	Role    string
	Content string
	Blocks  []ContentBlock
}

// HasBlocks reports whether the message has structured content blocks.
func (m TranscriptMessage) HasBlocks() bool {
	return len(m.Blocks) > 0
}

// TextContent extracts the plain text content from a message.
func (m TranscriptMessage) TextContent() string {
	if !m.HasBlocks() {
		return m.Content
	}
	var parts []string
	for _, b := range m.Blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Session holds metadata about a conversation session.
type Session struct {
	ID              string    `json:"id"`
	ProjectPath     string    `json:"project_path"`
	Title           string    `json:"title"`
	Model           string    `json:"model"`
	TotalTokens     int       `json:"total_tokens"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	MessageCount    int       `json:"message_count"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
	BranchPoint     int       `json:"branch_point,omitempty"`
	Tags            string    `json:"tags,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TagList returns the tags as a slice of strings.
func (s Session) TagList() []string {
	if s.Tags == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.Split(s.Tags, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// HasTag reports whether the session has the given tag (case-insensitive).
func (s Session) HasTag(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	for _, t := range s.TagList() {
		if strings.ToLower(t) == tag {
			return true
		}
	}
	return false
}

// APIModelInfo holds information about an available model from a provider API.
type APIModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// ModelPricing holds per-million-token pricing for a model.
type ModelPricing struct {
	InputPerMillion  float64 `json:"input"`
	OutputPerMillion float64 `json:"output"`
}
