package tui

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/hub"
)

// NodePicker is an interactive node selector overlay for hub connections.
type NodePicker struct {
	nodes       []*hub.Node
	filtered    []*hub.Node
	selectedIdx int
	filter      string
	active      bool
}

// NewNodePicker creates a picker with the given nodes.
func NewNodePicker(nodes []*hub.Node) *NodePicker {
	return &NodePicker{
		nodes:    nodes,
		filtered: nodes,
		active:   true,
	}
}

// IsActive reports whether the picker is currently shown.
func (p *NodePicker) IsActive() bool {
	return p != nil && p.active
}

// Dismiss closes the picker.
func (p *NodePicker) Dismiss() {
	p.active = false
}

// SelectedNode returns the currently highlighted node, or nil.
func (p *NodePicker) SelectedNode() *hub.Node {
	if len(p.filtered) == 0 {
		return nil
	}
	return p.filtered[p.selectedIdx]
}

// MoveUp moves the selection up.
func (p *NodePicker) MoveUp() {
	if p.selectedIdx > 0 {
		p.selectedIdx--
	}
}

// MoveDown moves the selection down.
func (p *NodePicker) MoveDown() {
	if p.selectedIdx < len(p.filtered)-1 {
		p.selectedIdx++
	}
}

// AppendFilter adds a rune to the filter.
func (p *NodePicker) AppendFilter(r rune) {
	p.filter += string(r)
	p.applyFilter()
}

// BackspaceFilter removes the last rune from the filter.
func (p *NodePicker) BackspaceFilter() {
	if len(p.filter) > 0 {
		runes := []rune(p.filter)
		p.filter = string(runes[:len(runes)-1])
		p.applyFilter()
	}
}

func (p *NodePicker) applyFilter() {
	if p.filter == "" {
		p.filtered = p.nodes
	} else {
		lower := strings.ToLower(p.filter)
		p.filtered = nil
		for _, n := range p.nodes {
			if strings.Contains(strings.ToLower(n.Name), lower) ||
				strings.Contains(strings.ToLower(n.Host), lower) ||
				strings.Contains(strings.ToLower(n.ID), lower) {
				p.filtered = append(p.filtered, n)
			}
		}
	}
	p.selectedIdx = 0
}

// View renders the picker as a string.
func (p *NodePicker) View(width int) string {
	if width < 40 {
		width = 40
	}

	var b strings.Builder

	b.WriteString(FooterHead.Render("Node Picker"))
	b.WriteString("\n")

	filterLine := "  Filter: " + p.filter
	b.WriteString(FooterMeta.Render(filterLine))
	b.WriteString(CursorStyle.Render("\u2588"))
	b.WriteString("\n\n")

	if len(p.filtered) == 0 {
		b.WriteString(FooterMeta.Render("  No matching nodes."))
		b.WriteString("\n")
	} else {
		const maxVisible = 10
		start := 0
		if p.selectedIdx >= maxVisible {
			start = p.selectedIdx - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(p.filtered) {
			end = len(p.filtered)
		}

		for i := start; i < end; i++ {
			n := p.filtered[i]
			indicator := "  "
			if i == p.selectedIdx {
				indicator = "> "
			}

			name := n.Name
			if len(name) > 16 {
				name = name[:13] + "..."
			}

			addr := fmt.Sprintf("%s:%d", n.Host, n.Port)
			if len(addr) > 22 {
				addr = addr[:19] + "..."
			}

			line := fmt.Sprintf("%s%-16s  %-22s  %-7s  %s",
				indicator, name, addr, string(n.Status), n.Version)

			if i == p.selectedIdx {
				b.WriteString(CompletionSelStyle.Render(line))
			} else {
				b.WriteString(FooterMeta.Render(line))
			}
			b.WriteString("\n")
		}

		if len(p.filtered) > maxVisible {
			more := fmt.Sprintf("  ... %d total", len(p.filtered))
			b.WriteString(FooterMeta.Render(more))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(FooterMeta.Render("  Enter=select  Esc=cancel"))
	b.WriteString("\n")

	return b.String()
}
