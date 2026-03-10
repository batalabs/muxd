package tools

import (
	"fmt"
	"testing"
)

func TestHubDispatchTool(t *testing.T) {
	tool := hubDispatchTool()

	if tool.Spec.Name != "hub_dispatch" {
		t.Fatalf("expected name hub_dispatch, got %s", tool.Spec.Name)
	}
	if len(tool.Spec.Required) != 2 {
		t.Fatalf("expected 2 required fields, got %d", len(tool.Spec.Required))
	}

	tests := []struct {
		name       string
		input      map[string]any
		dispatch   func(string, string) (string, error)
		wantErr    string
		wantResult string
	}{
		{
			name:    "returns error when HubDispatch is nil",
			input:   map[string]any{"node": "mynode", "prompt": "do stuff"},
			wantErr: "not connected to a hub",
		},
		{
			name:     "returns error on empty node",
			input:    map[string]any{"node": "", "prompt": "do stuff"},
			dispatch: func(string, string) (string, error) { return "", nil },
			wantErr:  "node is required",
		},
		{
			name:     "returns error on missing node",
			input:    map[string]any{"prompt": "do stuff"},
			dispatch: func(string, string) (string, error) { return "", nil },
			wantErr:  "node is required",
		},
		{
			name:     "returns error on empty prompt",
			input:    map[string]any{"node": "mynode", "prompt": ""},
			dispatch: func(string, string) (string, error) { return "", nil },
			wantErr:  "prompt is required",
		},
		{
			name:     "returns error on missing prompt",
			input:    map[string]any{"node": "mynode"},
			dispatch: func(string, string) (string, error) { return "", nil },
			wantErr:  "prompt is required",
		},
		{
			name:  "calls HubDispatch with correct args and returns result",
			input: map[string]any{"node": "linux-node", "prompt": "run tests"},
			dispatch: func(node, prompt string) (string, error) {
				if node != "linux-node" {
					return "", fmt.Errorf("unexpected node: %s", node)
				}
				if prompt != "run tests" {
					return "", fmt.Errorf("unexpected prompt: %s", prompt)
				}
				return "All 42 tests passed.", nil
			},
			wantResult: "All 42 tests passed.",
		},
		{
			name:  "propagates dispatch error",
			input: map[string]any{"node": "offline-node", "prompt": "do stuff"},
			dispatch: func(string, string) (string, error) {
				return "", fmt.Errorf("node is offline")
			},
			wantErr: "hub dispatch: node is offline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ToolContext{
				HubDispatch: tt.dispatch,
			}
			result, err := tool.Execute(tt.input, ctx)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if got := err.Error(); got != tt.wantErr && !contains(got, tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.wantResult {
				t.Fatalf("expected result %q, got %q", tt.wantResult, result)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
