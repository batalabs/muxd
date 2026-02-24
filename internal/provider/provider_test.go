package provider

import (
	"testing"
)

func TestResolveProviderAndModel(t *testing.T) {
	tests := []struct {
		name            string
		spec            string
		currentProvider string
		wantProvider    string
		wantModel       string
	}{
		{
			name:            "explicit openai prefix",
			spec:            "openai/gpt-4o",
			currentProvider: "anthropic",
			wantProvider:    "openai",
			wantModel:       "gpt-4o",
		},
		{
			name:            "explicit grok prefix",
			spec:            "grok/grok-4",
			currentProvider: "anthropic",
			wantProvider:    "grok",
			wantModel:       "grok-4",
		},
		{
			name:            "explicit xai prefix",
			spec:            "xai/grok-4",
			currentProvider: "anthropic",
			wantProvider:    "grok",
			wantModel:       "grok-4",
		},
		{
			name:            "explicit zai prefix",
			spec:            "zai/glm-5",
			currentProvider: "anthropic",
			wantProvider:    "zai",
			wantModel:       "glm-5",
		},
		{
			name:            "explicit mistral prefix",
			spec:            "mistral/mistral-small-latest",
			currentProvider: "anthropic",
			wantProvider:    "mistral",
			wantModel:       "mistral-small-latest",
		},
		{
			name:            "explicit anthropic prefix with alias",
			spec:            "anthropic/claude-sonnet",
			currentProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-sonnet-4-6",
		},
		{
			name:            "bare claude alias",
			spec:            "claude-sonnet",
			currentProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-sonnet-4-6",
		},
		{
			name:            "bare claude-opus",
			spec:            "claude-opus",
			currentProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-opus-4-6",
		},
		{
			name:            "bare claude model ID",
			spec:            "claude-3-7-sonnet-20250219",
			currentProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-3-7-sonnet-20250219",
		},
		{
			name:            "bare gpt model auto-detects openai",
			spec:            "gpt-4o",
			currentProvider: "anthropic",
			wantProvider:    "openai",
			wantModel:       "gpt-4o",
		},
		{
			name:            "bare o3 model auto-detects openai",
			spec:            "o3-mini",
			currentProvider: "anthropic",
			wantProvider:    "openai",
			wantModel:       "o3-mini",
		},
		{
			name:            "bare grok model auto-detects grok",
			spec:            "grok-4",
			currentProvider: "anthropic",
			wantProvider:    "grok",
			wantModel:       "grok-4",
		},
		{
			name:            "bare glm model auto-detects zai",
			spec:            "glm-5",
			currentProvider: "anthropic",
			wantProvider:    "zai",
			wantModel:       "glm-5",
		},
		{
			name:            "bare mistral model auto-detects mistral",
			spec:            "mistral-small-latest",
			currentProvider: "anthropic",
			wantProvider:    "mistral",
			wantModel:       "mistral-small-latest",
		},
		{
			name:            "empty spec returns current provider",
			spec:            "",
			currentProvider: "openai",
			wantProvider:    "openai",
			wantModel:       "",
		},
		{
			name:            "unknown bare model uses current provider",
			spec:            "my-custom-model",
			currentProvider: "ollama",
			wantProvider:    "ollama",
			wantModel:       "my-custom-model",
		},
		{
			name:            "google prefix",
			spec:            "google/gemini-pro",
			currentProvider: "anthropic",
			wantProvider:    "google",
			wantModel:       "gemini-pro",
		},
		{
			name:            "ollama prefix",
			spec:            "ollama/llama3",
			currentProvider: "anthropic",
			wantProvider:    "ollama",
			wantModel:       "llama3",
		},
		{
			name:            "explicit fireworks prefix",
			spec:            "fireworks/accounts/fireworks/models/llama-v3p1-8b-instruct",
			currentProvider: "anthropic",
			wantProvider:    "fireworks",
			wantModel:       "accounts/fireworks/models/llama-v3p1-8b-instruct",
		},
		{
			name:            "bare fireworks model auto-detects fireworks",
			spec:            "accounts/fireworks/models/llama-v3p1-70b-instruct",
			currentProvider: "anthropic",
			wantProvider:    "fireworks",
			wantModel:       "accounts/fireworks/models/llama-v3p1-70b-instruct",
		},
		{
			name:            "dot prefix strips to model ID",
			spec:            "anthropic.claude-sonnet-4-6",
			currentProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-sonnet-4-6",
		},
		{
			name:            "dot prefix with alias",
			spec:            "anthropic.claude-opus",
			currentProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-opus-4-6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotProv, gotModel := ResolveProviderAndModel(tt.spec, tt.currentProvider)
			if gotProv != tt.wantProvider {
				t.Errorf("provider = %q, want %q", gotProv, tt.wantProvider)
			}
			if gotModel != tt.wantModel {
				t.Errorf("model = %q, want %q", gotModel, tt.wantModel)
			}
		})
	}
}

func TestGetProvider(t *testing.T) {
	tests := []struct {
		name     string
		wantName string
		wantErr  bool
	}{
		{"anthropic", "anthropic", false},
		{"zai", "zai", false},
		{"grok", "grok", false},
		{"mistral", "mistral", false},
		{"openai", "openai", false},
		{"ollama", "ollama", false},
		{"fireworks", "fireworks", false},
		{"", "", true},
		{"unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := GetProvider(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Name() != tt.wantName {
				t.Errorf("Name() = %q, want %q", p.Name(), tt.wantName)
			}
		})
	}
}

func TestResolveProviderAndModel_localTaggedModelDefaultsToOllama(t *testing.T) {
	gotProv, gotModel := ResolveProviderAndModel("gemma3:4b", "anthropic")
	if gotProv != "ollama" {
		t.Fatalf("provider = %q, want %q", gotProv, "ollama")
	}
	if gotModel != "gemma3:4b" {
		t.Fatalf("model = %q, want %q", gotModel, "gemma3:4b")
	}
}

func TestNormalizeOpenAIStop(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"stop", "end_turn"},
		{"tool_calls", "tool_use"},
		{"length", "max_tokens"},
		{"content_filter", "end_turn"},
		{"", "end_turn"},
		{"something_else", "something_else"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeOpenAIStop(tt.input)
			if got != tt.want {
				t.Errorf("normalizeOpenAIStop(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToAnthropicTools(t *testing.T) {
	specs := []ToolSpec{
		{
			Name:        "test_tool",
			Description: "A test tool",
			Properties: map[string]ToolProp{
				"arg1": {Type: "string", Description: "First argument"},
				"arg2": {Type: "integer", Description: "Second argument"},
			},
			Required: []string{"arg1"},
		},
	}

	tools := toAnthropicTools(specs, "claude-sonnet-4-5-20250929")
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Name != "test_tool" {
		t.Errorf("Name = %q, want 'test_tool'", tool.Name)
	}
	if tool.InputSchema.Type != "object" {
		t.Errorf("Type = %q, want 'object'", tool.InputSchema.Type)
	}
	if len(tool.InputSchema.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(tool.InputSchema.Properties))
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "arg1" {
		t.Errorf("Required = %v, want [arg1]", tool.InputSchema.Required)
	}

	// Last tool should have cache_control for prompt caching.
	if tool.CacheControl == nil {
		t.Fatal("expected cache_control on last tool")
	}
	if tool.CacheControl.Type != "ephemeral" {
		t.Errorf("CacheControl.Type = %q, want 'ephemeral'", tool.CacheControl.Type)
	}
}

func TestToAnthropicTools_cacheOnLastOnly(t *testing.T) {
	specs := []ToolSpec{
		{Name: "a", Description: "first", Properties: map[string]ToolProp{"x": {Type: "string"}}, Required: []string{"x"}},
		{Name: "b", Description: "second", Properties: map[string]ToolProp{"y": {Type: "string"}}, Required: []string{"y"}},
		{Name: "c", Description: "third", Properties: map[string]ToolProp{"z": {Type: "string"}}, Required: []string{"z"}},
	}
	tools := toAnthropicTools(specs, "claude-sonnet-4-6")
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	// Only the last tool should have cache_control.
	for i, tool := range tools {
		if i < len(tools)-1 {
			if tool.CacheControl != nil {
				t.Errorf("tool[%d] %q should not have cache_control", i, tool.Name)
			}
		} else {
			if tool.CacheControl == nil || tool.CacheControl.Type != "ephemeral" {
				t.Errorf("last tool %q missing cache_control ephemeral", tool.Name)
			}
		}
	}
}

func TestToOpenAITools(t *testing.T) {
	specs := []ToolSpec{
		{
			Name:        "test_tool",
			Description: "A test tool",
			Properties: map[string]ToolProp{
				"arg1": {Type: "string", Description: "First argument"},
			},
			Required: []string{"arg1"},
		},
	}

	tools := toOpenAITools(specs)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Type != "function" {
		t.Errorf("Type = %q, want 'function'", tool.Type)
	}
	if tool.Function.Name != "test_tool" {
		t.Errorf("Name = %q, want 'test_tool'", tool.Function.Name)
	}
	if tool.Function.Description != "A test tool" {
		t.Errorf("Description = %q", tool.Function.Description)
	}
	if len(tool.Function.Parameters) == 0 {
		t.Error("expected non-empty parameters JSON")
	}
}

func TestToOpenAITools_empty(t *testing.T) {
	tools := toOpenAITools(nil)
	if tools != nil {
		t.Errorf("expected nil for empty specs, got %v", tools)
	}
}

func TestIsOpenAIChatModel(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"gpt-4o", true},
		{"gpt-4o-mini", true},
		{"gpt-3.5-turbo", true},
		{"o1-preview", true},
		{"o3-mini", true},
		{"o4-mini", true},
		{"dall-e-3", false},
		{"text-embedding-3-small", false},
		{"whisper-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := isOpenAIChatModel(tt.id)
			if got != tt.want {
				t.Errorf("isOpenAIChatModel(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
