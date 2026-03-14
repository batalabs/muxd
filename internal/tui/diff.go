package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	diffFileHeaderStyle = lipgloss.NewStyle().Bold(true).Faint(true)
	diffHunkHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Faint(true)
	diffDeleteStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	diffAddStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// RenderDiff applies Lipgloss styling to a unified diff string.
// Red for deletions, green for additions, cyan/dim for hunk headers,
// bold/dim for file headers. Lines are capped at width characters.
func RenderDiff(diff string, width int) string {
	if diff == "" {
		return ""
	}
	if width < 10 {
		width = 10
	}

	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		// Cap line length to avoid wrapping.
		if lipgloss.Width(line) > width {
			line = TruncateToWidth(line, width)
		}

		var styled string
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			styled = diffFileHeaderStyle.Render(line)
		case strings.HasPrefix(line, "@@"):
			styled = diffHunkHeaderStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			styled = diffDeleteStyle.Render(line)
		case strings.HasPrefix(line, "+"):
			styled = diffAddStyle.Render(line)
		default:
			styled = line
		}
		out = append(out, styled)
	}

	return strings.Join(out, "\n")
}
