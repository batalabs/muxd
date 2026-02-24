package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/batalabs/muxd/internal/tools"
)

// ToolPicker is an interactive overlay for enabling/disabling tools.
type ToolPicker struct {
	names       []string
	filtered    []string
	filter      string
	baseline    map[string]bool
	disabled    map[string]bool
	selectedIdx int
	active      bool
}

// NewToolPicker creates a tool picker with the given tool names and disabled set.
func NewToolPicker(names []string, disabled map[string]bool) *ToolPicker {
	copiedNames := append([]string(nil), names...)
	sort.Strings(copiedNames)
	baselineCopy := make(map[string]bool, len(disabled))
	disabledCopy := make(map[string]bool, len(disabled))
	for k, v := range disabled {
		if v {
			baselineCopy[k] = true
			disabledCopy[k] = true
		}
	}
	return &ToolPicker{
		names:    copiedNames,
		filtered: copiedNames,
		baseline: baselineCopy,
		disabled: disabledCopy,
		active:   true,
	}
}

func (p *ToolPicker) IsActive() bool {
	return p != nil && p.active
}

func (p *ToolPicker) Dismiss() {
	p.active = false
}

func (p *ToolPicker) MoveUp() {
	if p.selectedIdx > 0 {
		p.selectedIdx--
	}
}

func (p *ToolPicker) MoveDown() {
	if p.selectedIdx < len(p.filtered)-1 {
		p.selectedIdx++
	}
}

func (p *ToolPicker) SelectedName() string {
	if len(p.filtered) == 0 || p.selectedIdx < 0 || p.selectedIdx >= len(p.filtered) {
		return ""
	}
	return p.filtered[p.selectedIdx]
}

func (p *ToolPicker) ToggleSelected() {
	name := p.SelectedName()
	if name == "" {
		return
	}
	if p.disabled[name] {
		delete(p.disabled, name)
	} else {
		p.disabled[name] = true
	}
}

func (p *ToolPicker) DisabledMap() map[string]bool {
	out := make(map[string]bool, len(p.disabled))
	for k, v := range p.disabled {
		if v {
			out[k] = true
		}
	}
	return out
}

func (p *ToolPicker) Dirty() bool {
	if len(p.baseline) != len(p.disabled) {
		return true
	}
	for k := range p.baseline {
		if p.baseline[k] != p.disabled[k] {
			return true
		}
	}
	return false
}

func (p *ToolPicker) MarkApplied() {
	p.baseline = p.DisabledMap()
}

func (p *ToolPicker) ResetToBaseline() {
	p.disabled = make(map[string]bool, len(p.baseline))
	for k, v := range p.baseline {
		if v {
			p.disabled[k] = true
		}
	}
}

func (p *ToolPicker) AppendFilter(r rune) {
	p.filter += string(r)
	p.applyFilter()
}

func (p *ToolPicker) BackspaceFilter() {
	if len(p.filter) == 0 {
		return
	}
	rs := []rune(p.filter)
	p.filter = string(rs[:len(rs)-1])
	p.applyFilter()
}

func (p *ToolPicker) applyFilter() {
	if strings.TrimSpace(p.filter) == "" {
		p.filtered = append([]string(nil), p.names...)
		p.selectedIdx = 0
		return
	}
	needle := strings.ToLower(strings.TrimSpace(p.filter))
	p.filtered = nil
	for _, n := range p.names {
		display := strings.ToLower(tools.ToolDisplayName(n))
		if strings.Contains(display, needle) || strings.Contains(strings.ToLower(n), needle) {
			p.filtered = append(p.filtered, n)
		}
	}
	p.selectedIdx = 0
}

func (p *ToolPicker) View(width int) string {
	if width < 50 {
		width = 50
	}
	var b strings.Builder
	b.WriteString(FooterHead.Render("Tool Picker"))
	b.WriteString("\n")
	b.WriteString(FooterMeta.Render("  Enter/Space=toggle  a=apply  c=cancel  p=profile  Esc=close"))
	b.WriteString("\n")
	b.WriteString(FooterMeta.Render("  Filter: " + p.filter))
	b.WriteString(CursorStyle.Render("█"))
	b.WriteString("\n\n")

	if len(p.filtered) == 0 {
		b.WriteString(FooterMeta.Render("  No tools found."))
		b.WriteString("\n")
		return b.String()
	}

	const maxVisible = 12
	start := 0
	if p.selectedIdx >= maxVisible {
		start = p.selectedIdx - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(p.filtered) {
		end = len(p.filtered)
	}

	// Reserve space: "  " or "> " prefix (2), padding before state (3), state (8), risk tag (~10).
	// The rest is available for the tool name.
	maxNameWidth := width - 25
	if maxNameWidth < 20 {
		maxNameWidth = 20
	}
	if maxNameWidth > 40 {
		maxNameWidth = 40
	}

	for i := start; i < end; i++ {
		name := p.filtered[i]
		displayName := tools.ToolDisplayName(name)
		risk := tools.ToolRiskTags(name)
		riskLabel := ""
		if len(risk) > 0 {
			riskLabel = " [" + strings.Join(risk, ",") + "]"
		}
		state := "enabled"
		if p.disabled[name] {
			state = "disabled"
		}
		// Truncate long tool names with ellipsis.
		truncated := displayName
		if len(truncated) > maxNameWidth {
			truncated = truncated[:maxNameWidth-1] + "…"
		}
		nameFmt := fmt.Sprintf("%%-%ds", maxNameWidth)
		var line string
		if i == p.selectedIdx {
			line = fmt.Sprintf("> "+nameFmt+"   %-8s%s", truncated, state, riskLabel)
			b.WriteString(CompletionSelStyle.Render(line))
		} else {
			line = fmt.Sprintf("  "+nameFmt+"   %-8s%s", truncated, state, riskLabel)
			b.WriteString(FooterMeta.Render(line))
		}
		b.WriteString("\n")
	}

	if len(p.filtered) > maxVisible {
		b.WriteString(FooterMeta.Render(fmt.Sprintf("  ... %d shown (%d total)", len(p.filtered), len(p.names))))
		b.WriteString("\n")
	}
	if p.Dirty() {
		b.WriteString(FooterMeta.Render("  Pending changes not applied"))
		b.WriteString("\n")
	}

	return b.String()
}
