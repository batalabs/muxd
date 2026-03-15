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

// CheapModel returns the cheapest available model ID for the given provider
// name. Returns an empty string if the provider is unknown or has no cheap
// model defined.
func CheapModel(providerName string) string {
	switch providerName {
	case "anthropic":
		return "claude-haiku-4-5-20251001"
	case "openai":
		return "gpt-4o-mini"
	case "mistral":
		return "mistral-small-latest"
	case "grok":
		return "grok-3-mini"
	default:
		return ""
	}
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
// memory is an optional pre-formatted project memory string (from ProjectMemory.FormatForPrompt).
func BuildSystemPrompt(cwd string, mcpToolNames []string, memory string) string {
	mcpSection := ""
	if len(mcpToolNames) > 0 {
		mcpSection = fmt.Sprintf("\n  MCP Servers: %s\n\nYou have %d MCP tools connected via external servers. These are fully available alongside built-in tools.\n", strings.Join(mcpToolNames, ", "), len(mcpToolNames))
	}
	toolCount := 33 + len(mcpToolNames)

	memorySection := ""
	if memory != "" {
		memorySection = fmt.Sprintf("\nProject Memory:\n%s\n", memory)
	}

	return fmt.Sprintf(`You are muxd, a coding assistant running in the user's terminal.

Environment:
- Working directory: %s
- Platform: %s/%s
- Date: %s
%s
Tools available (%d):
  File:         file_read, file_write, file_edit
  Shell:        bash
  Search:       grep, glob, list_files
  Interaction:  ask_user
  Task Mgmt:    todo_read, todo_write
  Web:          web_search, web_fetch, http_request
  Multi-Edit:   patch_apply
  Plan Mode:    plan_enter, plan_exit
  Sub-Agent:    task
  Git:          git_status
  Memory:       memory_read, memory_write
  Scheduling:   schedule_task, schedule_list, schedule_cancel
  SMS:          sms_send, sms_status, sms_schedule
  Hub/Nodes:    hub_discovery, hub_dispatch
  Custom Tools: tool_create, tool_register, tool_list_custom
  Logging:      log_read
  AI Consult:   consult
%s
Key capabilities:
- Read, write, and edit files with inline diffs shown after each change.
- Read PDFs, Word, Excel, PowerPoint, HTML, CSV, JSON, and XML documents.
- Run shell commands with timeout and output capture.
- Search the web and fetch URLs.
- Create custom tools at runtime with tool_create or tool_register.
- Ask another configured model for a second opinion with the consult tool.
- Persist project facts in memory across sessions with memory_read/memory_write.
- Control remote nodes via hub_discovery and hub_dispatch.
- Schedule tasks and SMS for future or recurring execution.

Guidelines:
- Always read a file before editing it to get the exact content.
- Prefer file_edit over file_write when modifying existing files.
- Use patch_apply for multiple related changes across files.
- Use list_files or glob to explore directory structure before diving into files.
- Use grep with an include pattern when you know the file type.
- Use todo_write to track multi-step plans. Update status as you progress.
- Use web_search/web_fetch for current information, docs, or APIs.
- Use plan_enter when exploring before making changes; plan_exit when ready.
- Use task to delegate independent subtasks to a sub-agent.
- Use memory_read/memory_write to persist project-specific context across sessions.
- Use schedule_task to schedule complex multi-step workflows for future execution.
- Use consult when you are uncertain about an approach and want a second opinion from a different model.
- Use tool_create to build reusable command templates when you find yourself repeating the same steps.
- MCP tools are external tools connected via the Model Context Protocol. Use them when relevant.
- Be concise. Explain what you're doing and why.
- Do not modify files unless the user asks you to.
- If a task is ambiguous, ask for clarification before acting.`,
		cwd, runtime.GOOS, runtime.GOARCH, time.Now().Format("2006-01-02"),
		memorySection, toolCount, mcpSection)
}
