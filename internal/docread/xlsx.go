package docread

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// extractXLSX extracts text from an XLSX file.
// Each sheet is preceded by a "--- Sheet: Name ---" header.
// Rows are rendered as tab-separated values.
func extractXLSX(path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("opening XLSX: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	var sb strings.Builder
	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("reading sheet %q: %w", sheet, err)
		}
		sb.WriteString(fmt.Sprintf("--- Sheet: %s ---\n", sheet))
		for _, row := range rows {
			sb.WriteString(strings.Join(row, "\t"))
			sb.WriteByte('\n')
		}
	}
	return sb.String(), nil
}
