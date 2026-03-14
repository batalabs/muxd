package docread

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// extractPDF extracts text from a PDF file.
func extractPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening PDF: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	numPages := r.NumPage()
	for i := 1; i <= numPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		if numPages > 1 {
			fmt.Fprintf(&sb, "--- Page %d ---\n", i)
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
