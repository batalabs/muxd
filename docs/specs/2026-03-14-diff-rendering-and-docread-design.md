# Inline Diff Rendering & Document Reading

**Date:** 2026-03-14
**Status:** Approved

Two features that strengthen muxd as a coding agent: visual feedback on file changes, and the ability to read common document formats.

---

## Feature 1: Inline Diff Rendering

### Goal

When the agent edits a file, show a styled red/green unified diff inline in the chat stream. This gives the user immediate visibility into what changed without switching to a terminal or editor.

### Behavior

- **`file_edit`**: Always compute and display a unified diff (3 lines context) after a successful edit. Removed lines in red, added lines in green, context in gray.
- **`file_write` (overwrite)**: If the file existed before the write, compute and display a diff of old vs new content. If the file is new, show a brief "created `<path>` (N lines)" notice — no diff.
- **`file_write` (new file)**: No diff. Just a creation notice.
- **Config toggle**: `show_diffs` boolean in preferences. Default `true`. User can disable via `/config set show_diffs false`.

### Implementation

#### `internal/tui/diff.go` (new file)

Diff computation and rendering:

- `ComputeUnifiedDiff(oldText, newText, filename string) string` — produces a unified diff string with 3 lines of context. Use a pure-Go diff algorithm (Myers or patience). No external dependency needed — implement inline or use `github.com/sergi/go-diff`.
- `RenderDiff(diff string, width int) string` — applies Lipgloss styling:
  - `---` / `+++` header lines: bold, dimmed
  - `-` lines: red foreground
  - `+` lines: green foreground
  - `@@` hunk headers: cyan, dimmed
  - Context lines: default foreground
  - Line length capped at terminal width

#### `internal/tools/file_edit.go`

After a successful edit, compute the diff between old and new content. Include the rendered diff string in the tool result returned to the agent/TUI. The old content is already available (read before replace).

#### `internal/tools/file_write.go`

Before overwriting an existing file, read the old content. After writing, compute the diff. For new files, skip diff and return a creation notice.

#### `internal/tui/model.go`

In the `ToolResultMsg` handler, detect diff output in tool results and render with `RenderDiff()`. Respect `show_diffs` preference — if disabled, suppress diff output and show only a summary line ("edited `<path>`: N insertions, M deletions").

#### `internal/config/preferences.go`

Add `ShowDiffs bool` field (JSON: `show_diffs`). Default `true`.

### Edge Cases

- **Large diffs (>100 lines)**: Truncate with "... N more lines" and show a summary. Full diff available in runtime log.
- **Binary files**: Skip diff, show "binary file modified" notice.
- **Empty file → content**: Show as all-green (additions only).
- **Content → empty file**: Show as all-red (deletions only).

---

## Feature 2: Document Reading (Tier 1)

### Goal

Let the agent read common document formats — both when a user attaches a document to a message and when the agent encounters non-text files during `file_read`. Extracted text is passed to the LLM as regular text content.

### Supported Formats

| Extension | Format | Library | Output |
|-----------|--------|---------|--------|
| `.pdf` | PDF | `github.com/ledongthuc/pdf` | Plain text with page breaks (`--- Page N ---`) |
| `.docx` | Word | `github.com/nguyenthenguyen/docx` | Paragraphs as plain text |
| `.xlsx` | Excel | `github.com/xuri/excelize/v2` | Tab-separated values per sheet, prefixed with `--- Sheet: Name ---` |
| `.pptx` | PowerPoint | stdlib `archive/zip` + `encoding/xml` | Slide text in order, prefixed with `--- Slide N ---` |
| `.csv` | CSV | stdlib `encoding/csv` | Pass through raw (already text) |
| `.json` | JSON | stdlib `encoding/json` | Pretty-printed with indentation |
| `.xml` | XML | stdlib `encoding/xml` | Text content extracted, tags stripped |
| `.html`, `.htm` | HTML | `golang.org/x/net/html` | Text extracted, tags stripped, links preserved as `[text](url)` |

### Implementation

#### `internal/docread/docread.go` (new package)

Central dispatcher:

```go
// Extract reads a file and returns its text content.
// For supported document formats, it extracts text.
// Returns ErrUnsupportedFormat for unknown extensions.
func Extract(path string) (string, error)

// CanExtract reports whether the given file extension is supported.
func CanExtract(ext string) bool
```

Dispatches by `filepath.Ext(path)` to format-specific functions in the same package.

#### Format handler files

Each format gets its own file in `internal/docread/`:

- `pdf.go` — `extractPDF(path string) (string, error)`
- `docx.go` — `extractDOCX(path string) (string, error)`
- `xlsx.go` — `extractXLSX(path string) (string, error)`
- `pptx.go` — `extractPPTX(path string) (string, error)`
- `html.go` — `extractHTML(path string) (string, error)`

CSV, JSON, XML are simple enough to handle inline in `docread.go`.

#### `internal/docread/docread_test.go`

Table-driven tests with fixture files in `internal/docread/testdata/`:
- One small fixture per format (committed to repo)
- Test: extraction returns expected text
- Test: `CanExtract` returns correct results
- Test: unknown extension returns `ErrUnsupportedFormat`
- Test: corrupt/empty files return meaningful errors

#### `internal/tools/file_read.go`

Modify the file read tool:

1. Check if `docread.CanExtract(ext)` for the requested path.
2. If yes, call `docread.Extract(path)` instead of `os.ReadFile`.
3. Return extracted text with a header: `[Extracted from <filename> (<format>)]`.
4. Offset/limit parameters still apply to the extracted text (line-based).

#### `internal/tui/model.go`

Extend attachment handling (where images are currently detected):

1. When user pastes/attaches a file path, check if `docread.CanExtract(ext)`.
2. If yes, extract text via `docread.Extract()`.
3. Send as a text content block (not image block) — prepended with `[Document: <filename>]`.
4. Images continue to use the existing image block path.

### Size Limits

- **Max document size**: 10 MB file size limit. Reject larger files with a clear error.
- **Max extracted text**: 100,000 characters. Truncate with `... (truncated, <N> chars total)`.
- These limits prevent context window blowout from massive documents.

### Error Handling

- Corrupt files: return `fmt.Errorf("extracting %s: %w", path, err)` — agent sees the error and can tell the user.
- Password-protected PDFs/Office docs: return a clear "password-protected document" error.
- Missing libraries: all deps are pure Go, no CGo required.

---

## Dependencies

New Go module dependencies:

- `github.com/sergi/go-diff` — unified diff computation (or implement Myers inline)
- `github.com/ledongthuc/pdf` — PDF text extraction (pure Go)
- `github.com/nguyenthenguyen/docx` — DOCX reading (pure Go)
- `github.com/xuri/excelize/v2` — XLSX reading (pure Go)
- `golang.org/x/net/html` — HTML parsing (already likely in go.sum)

All are pure Go — no CGo, consistent with the project's cross-compilation requirement.

---

## Testing Strategy

- **Diff rendering**: Unit tests for `ComputeUnifiedDiff` and `RenderDiff` with table-driven cases (empty→content, content→empty, single line change, multi-hunk, large diff truncation).
- **Document reading**: Unit tests per format with small fixture files. Integration test: `file_read` tool with a PDF path returns extracted text.
- **Config**: Test that `show_diffs=false` suppresses diff output.

---

## What's NOT in scope

- OCR for scanned PDFs (Tier 3 — needs external binary)
- Old binary Office formats (.doc, .xls, .ppt)
- EPUB, RTF (Tier 2 — add later if needed)
- Side-by-side diff rendering
- Diff for tool results other than file_edit/file_write
