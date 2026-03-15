# Inline Diff Rendering & Document Reading Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add inline diff rendering for file edits and document reading support (PDF, Office, CSV, JSON, XML, HTML) to muxd.

**Architecture:** Two independent features sharing no code. Diff rendering hooks into the existing `file_edit` and `file_write` tool results, computing diffs before returning results. Document reading adds a new `internal/docread` package that `file_read` and the TUI attachment flow dispatch to for non-text file extensions.

**Tech Stack:** `github.com/sergi/go-diff` (unified diff), `github.com/ledongthuc/pdf` (PDF), `github.com/nguyenthenguyen/docx` (DOCX), `github.com/xuri/excelize/v2` (XLSX), `golang.org/x/net/html` (HTML), stdlib for CSV/JSON/XML/PPTX.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/tui/diff.go` | `ComputeUnifiedDiff()` and `RenderDiff()` — diff computation and Lipgloss styling |
| `internal/tui/diff_test.go` | Tests for diff computation and rendering |
| `internal/docread/docread.go` | `Extract()`, `CanExtract()` dispatcher + CSV/JSON/XML inline handlers |
| `internal/docread/docread_test.go` | Table-driven tests with fixture files |
| `internal/docread/pdf.go` | `extractPDF()` |
| `internal/docread/docx.go` | `extractDOCX()` |
| `internal/docread/xlsx.go` | `extractXLSX()` |
| `internal/docread/pptx.go` | `extractPPTX()` |
| `internal/docread/html.go` | `extractHTML()` |
| `internal/docread/testdata/` | Small fixture files (one per format) |

### Modified Files

| File | Change |
|------|--------|
| `internal/tools/tools.go:349-406` | `file_edit` — compute diff of old vs new, include in result |
| `internal/tools/tools.go:312-343` | `file_write` — read old content before write, compute diff for overwrites |
| `internal/tools/tools.go:237-306` | `file_read` — route document extensions through `docread.Extract()` |
| `internal/tui/render.go:817-842` | `FormatToolResult` — detect diff markers and render with colors |
| `internal/tui/model.go:1422-1490` | Extend attachment detection for document files |
| `internal/config/preferences.go` | Add `ShowDiffs bool` field |
| `go.mod` | Add new dependencies |

---

## Chunk 1: Inline Diff Rendering

### Task 1: Add `ShowDiffs` preference

**Files:**
- Modify: `internal/config/preferences.go`

- [ ] **Step 1: Add ShowDiffs field to Preferences struct**

In `internal/config/preferences.go`, add to the `Preferences` struct (around line 50, after the footer fields):

```go
ShowDiffs bool `json:"show_diffs,omitempty"`
```

Add `"show_diffs"` to the appropriate group in `ConfigGroupDefs` (the "Display" group or create one). Add a Get/Set case for it alongside the existing boolean preferences like `footer_tokens`.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add internal/config/preferences.go
git commit -m "feat: add show_diffs preference (default true)"
```

---

### Task 2: Diff computation and rendering

**Files:**
- Create: `internal/tui/diff.go`
- Create: `internal/tui/diff_test.go`

- [ ] **Step 1: Add go-diff dependency**

Run: `go get github.com/sergi/go-diff`

- [ ] **Step 2: Write failing tests for ComputeUnifiedDiff**

Create `internal/tui/diff_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestComputeUnifiedDiff(t *testing.T) {
	tests := []struct {
		name     string
		old      string
		new      string
		filename string
		wantHas  []string // substrings that must appear
		wantNot  []string // substrings that must NOT appear
	}{
		{
			name:     "single line change",
			old:      "line1\nline2\nline3\n",
			new:      "line1\nmodified\nline3\n",
			filename: "test.go",
			wantHas:  []string{"-line2", "+modified", "test.go"},
		},
		{
			name:     "empty to content",
			old:      "",
			new:      "new content\n",
			filename: "new.go",
			wantHas:  []string{"+new content"},
		},
		{
			name:     "content to empty",
			old:      "old content\n",
			new:      "",
			filename: "del.go",
			wantHas:  []string{"-old content"},
		},
		{
			name:     "no change",
			old:      "same\n",
			new:      "same\n",
			filename: "same.go",
			wantHas:  nil, // empty diff
		},
		{
			name:     "multi-hunk",
			old:      "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n",
			new:      "a\nB\nc\nd\ne\nf\ng\nH\ni\nj\n",
			filename: "multi.go",
			wantHas:  []string{"-b", "+B", "-h", "+H", "@@"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeUnifiedDiff(tt.old, tt.new, tt.filename)
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("diff missing %q\ngot:\n%s", want, got)
				}
			}
			for _, notWant := range tt.wantNot {
				if strings.Contains(got, notWant) {
					t.Errorf("diff should not contain %q\ngot:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestComputeUnifiedDiff_truncatesLargeDiffs(t *testing.T) {
	var old, new_ strings.Builder
	for i := 0; i < 200; i++ {
		old.WriteString("old line\n")
		new_.WriteString("new line\n")
	}
	diff := ComputeUnifiedDiff(old.String(), new_.String(), "big.go")
	lines := strings.Split(diff, "\n")
	if len(lines) > maxDiffLines+10 { // some slack for headers
		t.Errorf("diff should be truncated, got %d lines", len(lines))
	}
	if !strings.Contains(diff, "more lines") {
		t.Error("truncated diff should mention remaining lines")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestComputeUnifiedDiff -v`
Expected: FAIL — `ComputeUnifiedDiff` not defined

- [ ] **Step 4: Implement ComputeUnifiedDiff and RenderDiff**

Create `internal/tui/diff.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	difflib "github.com/sergi/go-diff/diffmatchpatch"
)

const maxDiffLines = 100

// ComputeUnifiedDiff produces a unified diff string between old and new text.
// Returns an empty string if there are no changes.
func ComputeUnifiedDiff(oldText, newText, filename string) string {
	if oldText == newText {
		return ""
	}

	dmp := difflib.New()
	a, b, c := dmp.DiffLinesToChars(oldText, newText)
	diffs := dmp.DiffMain(a, b, false)
	diffs = dmp.DiffCharsToLines(diffs, c)
	patches := dmp.PatchMake(oldText, diffs)

	if len(patches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", filename, filename))

	totalLines := 0
	truncated := false
	for _, p := range patches {
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", p.Start1+1, p.Length1, p.Start2+1, p.Length2))
		for _, d := range p.Diffs {
			lines := strings.Split(strings.TrimRight(d.Text, "\n"), "\n")
			for _, line := range lines {
				if totalLines >= maxDiffLines {
					truncated = true
					break
				}
				switch d.Type {
				case difflib.DiffEqual:
					sb.WriteString(" " + line + "\n")
				case difflib.DiffDelete:
					sb.WriteString("-" + line + "\n")
				case difflib.DiffInsert:
					sb.WriteString("+" + line + "\n")
				}
				totalLines++
			}
			if truncated {
				break
			}
		}
		if truncated {
			break
		}
	}

	if truncated {
		remaining := 0
		for _, p := range patches {
			for _, d := range p.Diffs {
				if d.Type != difflib.DiffEqual {
					remaining += strings.Count(d.Text, "\n")
				}
			}
		}
		sb.WriteString(fmt.Sprintf("... %d more lines (diff truncated)\n", remaining-maxDiffLines))
	}

	return sb.String()
}

var (
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	diffDelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Faint(true) // cyan dim
	diffHeaderStyle = lipgloss.NewStyle().Bold(true).Faint(true)
)

// RenderDiff applies Lipgloss styling to a unified diff string.
func RenderDiff(diff string, width int) string {
	if diff == "" {
		return ""
	}
	var sb strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		if len(line) > width && width > 0 {
			line = line[:width]
		}
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			sb.WriteString(diffHeaderStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			sb.WriteString(diffHunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			sb.WriteString(diffAddStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			sb.WriteString(diffDelStyle.Render(line))
		default:
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestComputeUnifiedDiff -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/diff.go internal/tui/diff_test.go go.mod go.sum
git commit -m "feat: add unified diff computation and styled rendering"
```

---

### Task 3: Hook diff into file_edit tool

**Files:**
- Modify: `internal/tools/tools.go:349-406` (file_edit section)

- [ ] **Step 1: Modify file_edit to compute and append diff**

In `internal/tools/tools.go`, in the `fileEditTool` function, after the successful write (around line 399-403), compute a diff and append it to the result string. The old content is already available from `os.ReadFile` at line 378.

Before the final return, add:

```go
diff := tui.ComputeUnifiedDiff(string(oldBytes), newContent, filepath.Base(path))
if diff != "" {
    result += "\n" + diff
}
```

Import `"github.com/batalabs/muxd/internal/tui"` and `"path/filepath"` if not already imported. Note: this creates a `tools → tui` dependency. If that's undesirable, put `ComputeUnifiedDiff` in a shared package like `internal/domain` instead.

**Alternative (preferred):** Put `ComputeUnifiedDiff` in `internal/tui/diff.go` but have `RenderDiff` separate. The tools package returns raw diff text (prefixed with a marker like `\n---DIFF---\n`), and the TUI renders it with colors. This avoids the `tools → tui` import cycle.

The approach: tools return raw unified diff text appended to the result with a `\n\x00DIFF\x00\n` sentinel. The TUI's `FormatToolResult` detects this sentinel, splits the diff out, and renders it with `RenderDiff`.

Move `ComputeUnifiedDiff` to a new file `internal/diff/diff.go` (no TUI dependency, no lipgloss). Keep `RenderDiff` in `internal/tui/diff.go`.

- [ ] **Step 2: Create internal/diff/diff.go**

Move `ComputeUnifiedDiff` and `maxDiffLines` to `internal/diff/diff.go` (package `diff`). This package has no TUI dependencies.

Update `internal/tui/diff.go` to only contain `RenderDiff` and the lipgloss styles, importing from `internal/diff`.

Update `internal/tui/diff_test.go` to import from `internal/diff` for `ComputeUnifiedDiff` tests, or move those tests to `internal/diff/diff_test.go`.

- [ ] **Step 3: Add diff sentinel constant**

In `internal/diff/diff.go`:

```go
const DiffSentinel = "\n\x00DIFF\x00\n"
```

- [ ] **Step 4: Modify file_edit to append diff**

In `internal/tools/tools.go` `fileEditTool`, after writing the file and before returning the result (around line 403):

```go
import "github.com/batalabs/muxd/internal/diff"

// After the existing result string is built:
d := diff.ComputeUnifiedDiff(string(oldBytes), newContent, filepath.Base(path))
if d != "" {
    result += diff.DiffSentinel + d
}
```

`oldBytes` is already available from line 378. `newContent` is the string after replacement.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tools/ -v`
Expected: PASS (existing tests still work, diff is just appended text)

- [ ] **Step 6: Commit**

```bash
git add internal/diff/ internal/tui/diff.go internal/tui/diff_test.go internal/tools/tools.go
git commit -m "feat: file_edit appends unified diff to tool result"
```

---

### Task 4: Hook diff into file_write tool

**Files:**
- Modify: `internal/tools/tools.go:312-343` (file_write section)

- [ ] **Step 1: Modify file_write to read old content before overwrite**

In the `fileWriteTool` function, before `os.WriteFile` (line 335), attempt to read existing file content:

```go
oldBytes, readErr := os.ReadFile(path)
isNew := readErr != nil // file doesn't exist = new file
```

After the successful write, compute and append diff:

```go
if !isNew {
    d := diff.ComputeUnifiedDiff(string(oldBytes), content, filepath.Base(path))
    if d != "" {
        result += diff.DiffSentinel + d
    }
}
```

For new files, the result stays as-is (just the "wrote N bytes" message).

- [ ] **Step 2: Run tests**

Run: `go test ./internal/tools/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tools/tools.go
git commit -m "feat: file_write shows diff for overwrites, creation notice for new files"
```

---

### Task 5: Render diffs in the TUI

**Files:**
- Modify: `internal/tui/render.go:817-842` (FormatToolResult)
- Modify: `internal/tui/model.go:523-528` (handleToolResult)

- [ ] **Step 1: Modify FormatToolResult to detect and render diffs**

In `internal/tui/render.go`, in the `FormatToolResult` function, before the existing truncation/formatting logic:

```go
import "github.com/batalabs/muxd/internal/diff"

// Check for diff sentinel in result
if idx := strings.Index(result, diff.DiffSentinel); idx >= 0 {
    toolOutput := result[:idx]
    diffText := result[idx+len(diff.DiffSentinel):]
    // Format tool output normally (existing logic)
    formatted := formatToolOutput(name, toolOutput, isError, width)
    // Append rendered diff
    formatted += "\n" + RenderDiff(diffText, width)
    return formatted
}
```

This may require extracting the existing formatting logic into a helper.

- [ ] **Step 2: Respect ShowDiffs preference**

In `handleToolResult` (model.go ~line 523), check `m.Prefs.ShowDiffs`. If false, strip the diff sentinel and diff content before passing to `FormatToolResult`. Since `ShowDiffs` defaults to zero-value `false`, we need the default to be `true`. Handle this by treating the zero-value as "show diffs":

```go
showDiffs := !m.Prefs.HideDiffs // or use a helper
```

Alternative: rename the field to `HideDiffs` (default false = show diffs). This avoids the zero-value problem. Update the preference accordingly.

- [ ] **Step 3: Run full test suite**

Run: `go test ./... `
Expected: PASS

- [ ] **Step 4: Manual test**

Run `muxd`, ask the agent to edit a file, verify the inline diff appears with red/green styling.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/model.go
git commit -m "feat: render inline diffs in TUI with red/green styling"
```

---

## Chunk 2: Document Reading

### Task 6: Add dependencies and create docread package scaffold

**Files:**
- Create: `internal/docread/docread.go`
- Modify: `go.mod`

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/ledongthuc/pdf
go get github.com/nguyenthenguyen/docx
go get github.com/xuri/excelize/v2
go get golang.org/x/net
```

- [ ] **Step 2: Write failing test for Extract dispatcher**

Create `internal/docread/docread_test.go`:

```go
package docread

import (
	"errors"
	"testing"
)

func TestCanExtract(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".pdf", true},
		{".docx", true},
		{".xlsx", true},
		{".pptx", true},
		{".csv", true},
		{".json", true},
		{".xml", true},
		{".html", true},
		{".htm", true},
		{".go", false},
		{".txt", false},
		{".png", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := CanExtract(tt.ext); got != tt.want {
				t.Errorf("CanExtract(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

func TestExtract_unsupportedFormat(t *testing.T) {
	_, err := Extract("test.xyz")
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/docread/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 4: Implement docread.go scaffold**

Create `internal/docread/docread.go`:

```go
package docread

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnsupportedFormat = errors.New("unsupported document format")

const (
	maxFileSize    = 10 * 1024 * 1024 // 10 MB
	maxExtractLen  = 100_000          // 100k chars
)

var extractors = map[string]func(string) (string, error){
	".pdf":  extractPDF,
	".docx": extractDOCX,
	".xlsx": extractXLSX,
	".pptx": extractPPTX,
	".html": extractHTML,
	".htm":  extractHTML,
}

// CanExtract reports whether the given file extension is a supported document format.
func CanExtract(ext string) bool {
	ext = strings.ToLower(ext)
	if _, ok := extractors[ext]; ok {
		return true
	}
	switch ext {
	case ".csv", ".json", ".xml":
		return true
	}
	return false
}

// Extract reads a document file and returns its text content.
func Extract(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("extracting %s: %w", path, err)
	}
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), maxFileSize)
	}

	if fn, ok := extractors[ext]; ok {
		text, err := fn(path)
		if err != nil {
			return "", fmt.Errorf("extracting %s: %w", path, err)
		}
		return truncate(text), nil
	}

	switch ext {
	case ".csv":
		return extractCSV(path)
	case ".json":
		return extractJSON(path)
	case ".xml":
		return extractXML(path)
	default:
		return "", ErrUnsupportedFormat
	}
}

func truncate(s string) string {
	if len(s) <= maxExtractLen {
		return s
	}
	return s[:maxExtractLen] + fmt.Sprintf("\n... (truncated, %d chars total)", len(s))
}

func extractCSV(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	r := csv.NewReader(f)
	var sb strings.Builder
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		sb.WriteString(strings.Join(record, "\t") + "\n")
	}
	return truncate(sb.String()), nil
}

func extractJSON(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		// Not valid JSON — return raw
		return truncate(string(data)), nil
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return truncate(string(data)), nil
	}
	return truncate(string(pretty)), nil
}

func extractXML(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if cd, ok := tok.(xml.CharData); ok {
			text := strings.TrimSpace(string(cd))
			if text != "" {
				sb.WriteString(text + "\n")
			}
		}
	}
	return truncate(sb.String()), nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/docread/ -run "TestCanExtract|TestExtract_unsupported" -v`
Expected: PASS (extractPDF etc. are not yet defined — add stubs)

- [ ] **Step 6: Add stubs for format handlers**

Create stub files so the package compiles. Each returns `ErrUnsupportedFormat` for now:

`internal/docread/pdf.go`:
```go
package docread

func extractPDF(path string) (string, error) {
	return "", ErrUnsupportedFormat
}
```

Same pattern for `docx.go`, `xlsx.go`, `pptx.go`, `html.go`.

- [ ] **Step 7: Run tests again**

Run: `go test ./internal/docread/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/docread/ go.mod go.sum
git commit -m "feat: add docread package scaffold with CSV/JSON/XML support"
```

---

### Task 7: Implement PDF extraction

**Files:**
- Modify: `internal/docread/pdf.go`
- Create: `internal/docread/testdata/sample.pdf`

- [ ] **Step 1: Create a small test PDF fixture**

Create a simple PDF test fixture. Use a Go test helper or commit a tiny pre-made PDF to `internal/docread/testdata/sample.pdf` containing text like "Hello PDF World".

- [ ] **Step 2: Write failing test**

Add to `internal/docread/docread_test.go`:

```go
func TestExtractPDF(t *testing.T) {
	text, err := Extract("testdata/sample.pdf")
	if err != nil {
		t.Fatalf("extractPDF: %v", err)
	}
	if !strings.Contains(text, "Hello") {
		t.Errorf("expected PDF text to contain 'Hello', got: %s", text)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/docread/ -run TestExtractPDF -v`
Expected: FAIL — returns ErrUnsupportedFormat (stub)

- [ ] **Step 4: Implement extractPDF**

In `internal/docread/pdf.go`:

```go
package docread

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

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
			sb.WriteString(fmt.Sprintf("--- Page %d ---\n", i))
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
```

- [ ] **Step 5: Run test**

Run: `go test ./internal/docread/ -run TestExtractPDF -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/docread/pdf.go internal/docread/testdata/
git commit -m "feat: add PDF text extraction"
```

---

### Task 8: Implement DOCX, XLSX, PPTX extraction

**Files:**
- Modify: `internal/docread/docx.go`, `xlsx.go`, `pptx.go`
- Create: `internal/docread/testdata/sample.docx`, `sample.xlsx`, `sample.pptx`

Follow the same TDD pattern for each format:

- [ ] **Step 1: Create fixture files and write tests**

Add test cases to `docread_test.go` for each format. Create small fixture files in `testdata/`.

- [ ] **Step 2: Implement extractDOCX**

```go
package docread

import (
	"strings"

	"github.com/nguyenthenguyen/docx"
)

func extractDOCX(path string) (string, error) {
	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	doc := r.Editable()
	return strings.TrimSpace(doc.GetContent()), nil
}
```

Note: `doc.GetContent()` may return XML markup. If so, strip tags or use the raw text extraction approach. Adjust based on what the library actually returns.

- [ ] **Step 3: Implement extractXLSX**

```go
package docread

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

func extractXLSX(path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var sb strings.Builder
	for _, sheet := range f.GetSheetList() {
		sb.WriteString(fmt.Sprintf("--- Sheet: %s ---\n", sheet))
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		for _, row := range rows {
			sb.WriteString(strings.Join(row, "\t") + "\n")
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
```

- [ ] **Step 4: Implement extractPPTX**

```go
package docread

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var slideFileRe = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

func extractPPTX(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	type slideEntry struct {
		num  int
		file *zip.File
	}
	var slides []slideEntry
	for _, f := range r.File {
		if m := slideFileRe.FindStringSubmatch(f.Name); m != nil {
			num := 0
			fmt.Sscanf(m[1], "%d", &num)
			slides = append(slides, slideEntry{num, f})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].num < slides[j].num })

	var sb strings.Builder
	for _, s := range slides {
		sb.WriteString(fmt.Sprintf("--- Slide %d ---\n", s.num))
		text, err := extractXMLText(s.file)
		if err != nil {
			continue
		}
		sb.WriteString(text + "\n")
	}
	return sb.String(), nil
}

func extractXMLText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			text := strings.TrimSpace(string(cd))
			if text != "" {
				sb.WriteString(text + "\n")
			}
		}
	}
	return sb.String(), nil
}
```

- [ ] **Step 5: Run all docread tests**

Run: `go test ./internal/docread/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/docread/
git commit -m "feat: add DOCX, XLSX, PPTX text extraction"
```

---

### Task 9: Implement HTML extraction

**Files:**
- Modify: `internal/docread/html.go`
- Create: `internal/docread/testdata/sample.html`

- [ ] **Step 1: Create fixture and write test**

`testdata/sample.html`:
```html
<html><body><h1>Title</h1><p>Hello <a href="https://example.com">world</a></p></body></html>
```

Test expects output containing "Title", "Hello", "[world](https://example.com)".

- [ ] **Step 2: Implement extractHTML**

```go
package docread

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
)

func extractHTML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text + " ")
			}
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return // skip
			case "br", "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr":
				sb.WriteString("\n")
			case "a":
				// Extract link text and href
				href := ""
				for _, a := range n.Attr {
					if a.Key == "href" {
						href = a.Val
						break
					}
				}
				if href != "" {
					var linkText strings.Builder
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.TextNode {
							linkText.WriteString(c.Data)
						}
					}
					sb.WriteString(fmt.Sprintf("[%s](%s) ", strings.TrimSpace(linkText.String()), href))
					return // don't recurse into children again
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.TrimSpace(sb.String()), nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/docread/ -run TestExtractHTML -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/docread/html.go internal/docread/testdata/sample.html
git commit -m "feat: add HTML text extraction with link preservation"
```

---

### Task 10: Hook docread into file_read tool

**Files:**
- Modify: `internal/tools/tools.go:237-306` (file_read section)

- [ ] **Step 1: Modify file_read to detect document extensions**

In `fileReadTool`, after the path validation and before `os.ReadFile` (around line 269), add:

```go
import "github.com/batalabs/muxd/internal/docread"

ext := strings.ToLower(filepath.Ext(path))
if docread.CanExtract(ext) {
    text, err := docread.Extract(path)
    if err != nil {
        return "", fmt.Errorf("reading document: %w", err)
    }
    header := fmt.Sprintf("[Extracted from %s (%s)]\n\n", filepath.Base(path), strings.TrimPrefix(ext, "."))
    result := header + text
    // Apply offset/limit to extracted text lines
    lines := strings.Split(result, "\n")
    if offset > 0 && offset < len(lines) {
        lines = lines[offset:]
    }
    if limit > 0 && limit < len(lines) {
        lines = lines[:limit]
    }
    return strings.Join(lines, "\n"), nil
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/tools/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tools/tools.go
git commit -m "feat: file_read routes document extensions through docread"
```

---

### Task 11: Hook docread into TUI attachment flow

**Files:**
- Modify: `internal/tui/model.go:1422-1490` (handleSubmit attachment section)

- [ ] **Step 1: Extend attachment detection for documents**

In the `handleSubmit` method, after the image extraction logic (around line 1423), add document detection. When `ExtractImagePaths` returns paths that are documents (not images), extract text and create text content blocks instead of image blocks.

Alternatively, modify the path extraction to also detect document extensions, then branch:

```go
// After extracting image paths, check for document paths too
for _, p := range imgPaths {
    ext := strings.ToLower(filepath.Ext(p))
    if docread.CanExtract(ext) {
        text, err := docread.Extract(p)
        if err != nil {
            continue
        }
        header := fmt.Sprintf("[Document: %s]", filepath.Base(p))
        images = append(images, daemon.SubmitImage{
            // Send as text, not image
        })
        // OR: append to remainingText
        remainingText = header + "\n" + text + "\n\n" + remainingText
    } else {
        // existing image handling
    }
}
```

The exact integration depends on how `ExtractImagePaths` works. The key behavior: document paths get extracted as text, prepended to the message. Image paths continue the existing base64 flow.

- [ ] **Step 2: Update ExtractImagePaths to also match document extensions**

In `internal/tools/image.go`, extend the regex or add a separate function `ExtractDocPaths` that matches document extensions (`.pdf`, `.docx`, `.xlsx`, `.pptx`, `.html`, `.csv`, `.json`, `.xml`).

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Manual test**

Run `muxd`, type a message with a PDF path (e.g., `read this /path/to/doc.pdf`), verify the extracted text is sent to the model.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tools/image.go
git commit -m "feat: TUI attachment flow supports document files via docread"
```

---

### Task 12: Final integration test and cleanup

- [ ] **Step 1: Run full test suite with race detector**

Run: `go test -race ./...`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: Clean

- [ ] **Step 3: Build**

Run: `go build -o muxd.exe .`
Expected: Clean build

- [ ] **Step 4: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: final cleanup for diff rendering and docread"
```
