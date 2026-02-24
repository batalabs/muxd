package provider

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/domain"
)

// ---------------------------------------------------------------------------
// Model alias resolution
// ---------------------------------------------------------------------------

// DefaultAnthropicModel is the fallback model ID for the Anthropic provider
// when a user specifies "anthropic" without a specific model name.
const DefaultAnthropicModel = "claude-sonnet-4-6"

// ModelAliases maps user-friendly names to Anthropic API model IDs.
var ModelAliases = map[string]string{
	"claude-sonnet": "claude-sonnet-4-6",
	"claude-haiku":  "claude-haiku-4-5-20251001",
	"claude-opus":   "claude-opus-4-6",
}

// ResolveModel maps user-friendly names to Anthropic API model IDs.
func ResolveModel(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return DefaultAnthropicModel
	}
	trimmed = strings.TrimPrefix(trimmed, "anthropic/")
	trimmed = strings.TrimPrefix(trimmed, "anthropic.")
	lower := strings.ToLower(trimmed)
	if resolved, ok := ModelAliases[lower]; ok {
		return resolved
	}
	return trimmed
}

// ---------------------------------------------------------------------------
// Model pricing
// ---------------------------------------------------------------------------

// PricingMap is populated at startup from ~/.config/muxd/pricing.json,
// falling back to built-in defaults. Use SetPricingMap() from main.
var PricingMap map[string]domain.ModelPricing

// SetPricingMap sets the global pricing map.
func SetPricingMap(m map[string]domain.ModelPricing) {
	PricingMap = m
}

// ModelCost returns the estimated cost in USD for a given token count.
func ModelCost(modelID string, inputTokens, outputTokens int) float64 {
	p, ok := PricingMap[modelID]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1_000_000)*p.InputPerMillion +
		(float64(outputTokens)/1_000_000)*p.OutputPerMillion
}

// ModelCostWithCache returns a cache-adjusted cost estimate.
// Assumptions:
// - cache_read_input_tokens are billed at 10% of normal input.
// - cache_creation_input_tokens are billed at normal input rates.
func ModelCostWithCache(modelID string, inputTokens, outputTokens, cacheCreationInputTokens, cacheReadInputTokens int) float64 {
	// cacheCreationInputTokens reserved for future billing refinement.
	effectiveInput := inputTokens - cacheReadInputTokens + int(math.Round(float64(cacheReadInputTokens)*0.10))
	if effectiveInput < 0 {
		effectiveInput = 0
	}
	return ModelCost(modelID, effectiveInput, outputTokens)
}

// ---------------------------------------------------------------------------
// System prompt
// ---------------------------------------------------------------------------

// BuildSystemPrompt returns the system prompt for the given working directory.
// mcpToolNames is an optional list of MCP tool names available to the agent.
func BuildSystemPrompt(cwd string, mcpToolNames []string) string {
	mcpSection := ""
	if len(mcpToolNames) > 0 {
		mcpSection = fmt.Sprintf("\n  MCP:         %s\n", strings.Join(mcpToolNames, ", "))
	}
	toolCount := 23 + len(mcpToolNames)

	return fmt.Sprintf(`You are muxd, a coding assistant running in the user's terminal.

Environment:
- Working directory: %s
- Platform: %s/%s
- Date: %s

Tools available (%d):
  File:        file_read, file_write, file_edit
  Shell:       bash
  Search:      grep, list_files
  Interaction: ask_user
  Task Mgmt:   todo_read, todo_write
  Web:         web_search (Brave), web_fetch
  X/Twitter:   x_post, x_search, x_mentions, x_reply, x_schedule, x_schedule_list, x_schedule_update, x_schedule_cancel
  Multi-Edit:  patch_apply
  Plan Mode:   plan_enter, plan_exit
  Sub-Agent:   task
  Git:         git_status%s
Guidelines:
- Always read a file before editing it to get the exact content.
- Prefer file_edit over file_write when modifying existing files.
- Use patch_apply for multiple related changes across files.
- Use list_files to explore directory structure before diving into files.
- Use grep with an include pattern when you know the file type.
- Use todo_write to track multi-step plans. Update status as you progress.
- Use web_search/web_fetch for current information, docs, or APIs.
- Use plan_enter when exploring before making changes; plan_exit when ready.
- Use task to delegate independent subtasks to a sub-agent.
- MCP tools are external tools connected via the Model Context Protocol. Use them when relevant — they extend your capabilities beyond the built-in tools.
- Be concise. Explain what you're doing and why.
- Do not modify files unless the user asks you to.
- If a task is ambiguous, ask for clarification before acting.`,
		cwd, runtime.GOOS, runtime.GOARCH, time.Now().Format("2006-01-02"),
		toolCount, mcpSection)
}
