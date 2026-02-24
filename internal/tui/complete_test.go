package tui

import (
	"slices"
	"testing"

	"github.com/batalabs/muxd/internal/provider"
)

func TestComputeCompletions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		extraIDs []string
		want     []string
	}{
		{
			name:  "bare slash shows all commands",
			input: "/",
			want:  SlashCommands,
		},
		{
			name:  "partial match filters commands",
			input: "/ne",
			want:  []string{"/new"},
		},
		{
			name:  "exact match returns single command",
			input: "/clear",
			want:  []string{"/clear"},
		},
		{
			name:  "config subcommands",
			input: "/config ",
			want:  []string{"/config messaging", "/config models", "/config reset", "/config set", "/config show", "/config theme", "/config tools"},
		},
		{
			name:  "config partial subcommand",
			input: "/config s",
			want:  []string{"/config set", "/config show"},
		},
		{
			name:  "config partial subcommand m",
			input: "/config m",
			want:  []string{"/config messaging", "/config models"},
		},
		{
			name:  "config set shows keys",
			input: "/config set ",
			want: func() []string {
				out := make([]string, len(ConfigKeys))
				for i, k := range ConfigKeys {
					out[i] = "/config set " + k
				}
				return out
			}(),
		},
		{
			name:  "config set partial key",
			input: "/config set footer.c",
			want:  []string{"/config set footer.cost", "/config set footer.cwd"},
		},
		{
			name:  "schedule subcommands",
			input: "/schedule ",
			want:  []string{"/schedule add", "/schedule add-task", "/schedule list", "/schedule cancel"},
		},
		{
			name:  "schedule add tool names",
			input: "/schedule add x_",
			want:  []string{"/schedule add x_mentions", "/schedule add x_post", "/schedule add x_reply", "/schedule add x_schedule", "/schedule add x_schedule_cancel", "/schedule add x_schedule_list", "/schedule add x_schedule_update", "/schedule add x_search"},
		},
		{
			name:  "config set model shows model aliases",
			input: "/config set model ",
			want: func() []string {
				names := ModelAliasNames()
				out := make([]string, len(names))
				for i, n := range names {
					out[i] = "/config set model " + n
				}
				return out
			}(),
		},
		{
			name:  "config set model partial alias filters",
			input: "/config set model claude-h",
			want:  []string{"/config set model claude-haiku"},
		},
		{
			name:     "config set model includes extra API model IDs",
			input:    "/config set model ",
			extraIDs: []string{"claude-sonnet-4-20250514", "claude-4-custom"},
			want: func() []string {
				names := ModelAliasNames()
				out := make([]string, len(names))
				for i, n := range names {
					out[i] = "/config set model " + n
				}
				out = append(out, "/config set model claude-sonnet-4-20250514", "/config set model claude-4-custom")
				return out
			}(),
		},
		{
			name:     "config set model deduplicates alias keys",
			input:    "/config set model ",
			extraIDs: []string{"claude-haiku"},
			want: func() []string {
				names := ModelAliasNames()
				out := make([]string, len(names))
				for i, n := range names {
					out[i] = "/config set model " + n
				}
				return out
			}(),
		},
		{
			name:     "config set model partial filters extra IDs too",
			input:    "/config set model claude-4",
			extraIDs: []string{"claude-4-custom", "gemini-pro"},
			want:     []string{"/config set model claude-4-custom"},
		},
		{
			name:  "non-slash input returns nil",
			input: "hello",
			want:  nil,
		},
		{
			name:  "empty input returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "no match returns empty",
			input: "/zzz",
			want:  nil,
		},
		{
			name:  "case insensitive slash command",
			input: "/NE",
			want:  []string{"/new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeCompletions(tt.input, tt.extraIDs)
			if !slices.Equal(got, tt.want) {
				t.Errorf("ComputeCompletions(%q, %v)\n  got:  %v\n  want: %v", tt.input, tt.extraIDs, got, tt.want)
			}
		})
	}
}

func TestModelAliasNames(t *testing.T) {
	names := ModelAliasNames()
	if len(names) != len(provider.ModelAliases) {
		t.Fatalf("expected %d names, got %d", len(provider.ModelAliases), len(names))
	}
	// Verify sorted order.
	if !slices.IsSorted(names) {
		t.Errorf("ModelAliasNames() not sorted: %v", names)
	}
	// Verify all keys present.
	for k := range provider.ModelAliases {
		if !slices.Contains(names, k) {
			t.Errorf("missing alias key: %s", k)
		}
	}
}

func TestFilterByPrefix(t *testing.T) {
	tests := []struct {
		name       string
		candidates []string
		prefix     string
		partial    string
		want       []string
	}{
		{
			name:       "empty partial matches all",
			candidates: []string{"a", "b", "c"},
			prefix:     "pre ",
			partial:    "",
			want:       []string{"pre a", "pre b", "pre c"},
		},
		{
			name:       "partial filters",
			candidates: []string{"alpha", "beta", "gamma"},
			prefix:     "",
			partial:    "al",
			want:       []string{"alpha"},
		},
		{
			name:       "no match",
			candidates: []string{"alpha", "beta"},
			prefix:     "",
			partial:    "zzz",
			want:       nil,
		},
		{
			name:       "case insensitive",
			candidates: []string{"Alpha", "beta"},
			prefix:     "",
			partial:    "ALPHA",
			want:       []string{"Alpha"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByPrefix(tt.candidates, tt.prefix, tt.partial)
			if !slices.Equal(got, tt.want) {
				t.Errorf("FilterByPrefix(%v, %q, %q)\n  got:  %v\n  want: %v",
					tt.candidates, tt.prefix, tt.partial, got, tt.want)
			}
		})
	}
}

func TestCommandExpectsArgs(t *testing.T) {
	tests := []struct {
		name       string
		completion string
		want       bool
	}{
		{"config alone expects args", "/config", true},
		{"config set expects args", "/config set", true},
		{"config set model expects args", "/config set model", true},
		{"config set model with value does not", "/config set model claude-sonnet", false},
		{"config set other key does not", "/config set footer.tokens", false},
		{"config show does not", "/config show", false},
		{"continue expects args", "/continue", true},
		{"resume expects args", "/resume", true},
		{"clear does not", "/clear", false},
		{"help does not", "/help", false},
		{"schedule expects args", "/schedule", true},
		{"schedule add expects args", "/schedule add", true},
		{"schedule list does not", "/schedule list", false},
		{"remember expects args", "/remember", true},
		{"remember with key does not", "/remember auth JWT", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommandExpectsArgs(tt.completion)
			if got != tt.want {
				t.Errorf("CommandExpectsArgs(%q) = %v, want %v", tt.completion, got, tt.want)
			}
		})
	}
}
