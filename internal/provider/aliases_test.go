package provider

import (
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
)

func TestResolveModel(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"", DefaultAnthropicModel},
		{"  ", DefaultAnthropicModel},
		{"claude-sonnet", "claude-sonnet-4-6"},
		{"claude-haiku", "claude-haiku-4-5-20251001"},
		{"claude-opus", "claude-opus-4-6"},
		{"Claude-Sonnet", "claude-sonnet-4-6"},
		{"anthropic/claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"anthropic.claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"custom-model-id", "custom-model-id"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ResolveModel(tt.input); got != tt.expect {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestModelCost(t *testing.T) {
	orig := PricingMap
	defer func() { PricingMap = orig }()

	PricingMap = map[string]domain.ModelPricing{
		"test-model": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	}

	tests := []struct {
		name   string
		model  string
		input  int
		output int
		expect float64
	}{
		{"known model", "test-model", 1_000_000, 1_000_000, 18.0},
		{"unknown model", "unknown", 1_000_000, 1_000_000, 0},
		{"zero tokens", "test-model", 0, 0, 0},
		{"only input", "test-model", 1_000_000, 0, 3.0},
		{"only output", "test-model", 0, 1_000_000, 15.0},
		{"fractional", "test-model", 500_000, 100_000, 3.0*0.5 + 15.0*0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ModelCost(tt.model, tt.input, tt.output)
			if got != tt.expect {
				t.Errorf("ModelCost() = %f, want %f", got, tt.expect)
			}
		})
	}
}

func TestModelCostWithCache(t *testing.T) {
	orig := PricingMap
	defer func() { PricingMap = orig }()

	PricingMap = map[string]domain.ModelPricing{
		"test-model": {InputPerMillion: 10.0, OutputPerMillion: 30.0},
	}

	t.Run("cache discount applied", func(t *testing.T) {
		// 1M input, 500K cache read -> effective = 1M - 500K + 50K = 550K
		cost := ModelCostWithCache("test-model", 1_000_000, 0, 0, 500_000)
		expected := (550_000.0 / 1_000_000) * 10.0
		if cost != expected {
			t.Errorf("cost = %f, want %f", cost, expected)
		}
	})

	t.Run("negative effective clamped to zero", func(t *testing.T) {
		// 100K input, 1M cache read -> would be negative, clamped to 0
		cost := ModelCostWithCache("test-model", 100_000, 0, 0, 1_000_000)
		if cost != 0 {
			t.Errorf("cost = %f, want 0 (clamped)", cost)
		}
	})
}

func TestSetPricingMap(t *testing.T) {
	orig := PricingMap
	defer func() { PricingMap = orig }()

	m := map[string]domain.ModelPricing{"x": {InputPerMillion: 1.0}}
	SetPricingMap(m)
	if PricingMap["x"].InputPerMillion != 1.0 {
		t.Error("SetPricingMap did not set map")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	t.Run("without MCP tools", func(t *testing.T) {
		prompt := BuildSystemPrompt("/tmp/project", nil)
		if !strings.Contains(prompt, "/tmp/project") {
			t.Error("expected cwd in prompt")
		}
		if !strings.Contains(prompt, "Tools available (23)") {
			t.Error("expected 23 tools")
		}
		if strings.Contains(prompt, "MCP:") {
			t.Error("should not contain MCP section without tools")
		}
	})

	t.Run("with MCP tools", func(t *testing.T) {
		prompt := BuildSystemPrompt("/tmp", []string{"mcp__fs__read", "mcp__fs__write"})
		if !strings.Contains(prompt, "Tools available (25)") {
			t.Error("expected 25 tools (23 + 2 MCP)")
		}
		if !strings.Contains(prompt, "MCP:") {
			t.Error("expected MCP section")
		}
		if !strings.Contains(prompt, "mcp__fs__read") {
			t.Error("expected MCP tool names in prompt")
		}
	})
}
