package docread

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nguyenthenguyen/docx"
)

// xmlTagRe matches XML tags for stripping.
var xmlTagRe = regexp.MustCompile(`<[^>]+>`)

// extractDOCX extracts text from a DOCX file.
func extractDOCX(path string) (string, error) {
	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", fmt.Errorf("opening DOCX: %w", err)
	}
	defer r.Close()

	doc := r.Editable()
	content := doc.GetContent()

	// GetContent returns XML — strip tags and clean up whitespace.
	text := xmlTagRe.ReplaceAllString(content, "")
	// Collapse multiple blank lines.
	lines := strings.Split(text, "\n")
	var sb strings.Builder
	blank := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blank++
			if blank <= 1 {
				sb.WriteByte('\n')
			}
		} else {
			blank = 0
			sb.WriteString(trimmed)
			sb.WriteByte('\n')
		}
	}
	return sb.String(), nil
}
