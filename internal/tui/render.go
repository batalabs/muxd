package tui

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/lipgloss"

	"github.com/batalabs/muxd/internal/domain"
)

var openFenceRe = regexp.MustCompile("([^\\n])```([A-Za-z0-9_-]*)")
var numberedListRe = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.+)`)
var inlineCodeRe = regexp.MustCompile("`([^`]+)`")
var boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
var strikethroughRe = regexp.MustCompile(`~~(.+?)~~`)
var linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
var hrRe = regexp.MustCompile(`^[-*_]{3,}\s*$`)
var tableRowRe = regexp.MustCompile(`^\s*\|(.+)\|\s*$`)              // rows with outer pipes
var tableRowLooseRe = regexp.MustCompile(`^[^|]*\|[^|]*(\|[^|]*)+$`) // rows without outer pipes (2+ cells)
var tableSepRe = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$`)

var knownCodeLangs = map[string]bool{
	"python": true, "py": true, "javascript": true, "js": true, "typescript": true, "ts": true,
	"go": true, "rust": true, "java": true, "c": true, "cpp": true, "c++": true, "csharp": true,
	"cs": true, "json": true, "yaml": true, "yml": true, "bash": true, "sh": true, "shell": true,
	"sql": true, "html": true, "css": true, "xml": true, "markdown": true, "md": true,
}

// WrapWords splits s into lines that fit within width, breaking at word
// boundaries. Words longer than width are hard-broken.
func WrapWords(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 8)
	cur := ""
	for _, word := range parts {
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if len(next) <= width {
			cur = next
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		for len(word) > width {
			lines = append(lines, word[:width])
			word = word[width:]
		}
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// RenderAssistantLines converts markdown-ish assistant text into styled,
// word-wrapped terminal lines.
func RenderAssistantLines(content string, width int) []string {
	if width < 20 {
		width = 20
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = openFenceRe.ReplaceAllString(normalized, "$1\n```$2")
	rawLines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(rawLines)+8)
	inCode := false
	codeLang := ""
	firstCodeLinePendingLang := false
	codeBuf := make([]string, 0, 32)

	// Table accumulation state.
	inTable := false
	var tableHeaders []string
	var tableRows [][]string

	flushTable := func() {
		if len(tableHeaders) > 0 && len(tableRows) > 0 {
			out = append(out, RenderTable(tableHeaders, tableRows, width)...)
		}
		inTable = false
		tableHeaders = nil
		tableRows = nil
	}

	for i, raw := range rawLines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		// --- Code fence handling (highest priority) ---
		if strings.HasPrefix(trimmed, "```") {
			if inTable {
				flushTable()
			}
			if !inCode {
				inCode = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				firstCodeLinePendingLang = codeLang == ""
				if codeLang == "" {
					codeLang = "text"
				}
				codeBuf = codeBuf[:0]
			} else {
				out = append(out, renderHighlightedCodeBlock(codeLang, strings.Join(codeBuf, "\n"), width)...)
				inCode = false
				codeLang = ""
				firstCodeLinePendingLang = false
				codeBuf = codeBuf[:0]
			}
			continue
		}

		if inCode {
			if firstCodeLinePendingLang {
				candidate := strings.ToLower(strings.TrimSpace(trimmed))
				if knownCodeLangs[candidate] {
					codeLang = candidate
					firstCodeLinePendingLang = false
					continue
				}
				firstCodeLinePendingLang = false
			}
			codeBuf = append(codeBuf, line)
			continue
		}

		// --- Table handling ---
		isTableRow := tableRowRe.MatchString(trimmed) || tableRowLooseRe.MatchString(trimmed)

		if inTable {
			if tableSepRe.MatchString(trimmed) {
				// Skip separator line (already consumed headers).
				continue
			}
			if isTableRow {
				cells := ParseTableRow(trimmed)
				// Pad or truncate to match header count.
				for len(cells) < len(tableHeaders) {
					cells = append(cells, "")
				}
				if len(cells) > len(tableHeaders) {
					cells = cells[:len(tableHeaders)]
				}
				tableRows = append(tableRows, cells)
				continue
			}
			// Non-table line while in table. During streaming a new row
			// may be arriving incomplete (not enough pipes yet). If this
			// is the last non-empty line, skip it so it doesn't flash as
			// plain text below the table.
			if isLastNonEmptyLine(rawLines, i) {
				continue
			}
			// Otherwise the table is done -- flush and fall through.
			flushTable()
		}

		// Detect table start: current line contains pipes and next line is a separator.
		if !inTable && isTableRow {
			if i+1 < len(rawLines) && tableSepRe.MatchString(strings.TrimSpace(rawLines[i+1])) {
				inTable = true
				tableHeaders = ParseTableRow(trimmed)
				tableRows = nil
				continue
			}
			// During streaming the header may arrive before the separator.
			// If this pipe-containing line is the last non-empty line, skip
			// it so it doesn't flash as plain text. It will render properly
			// once the separator arrives on the next delta.
			if isLastNonEmptyLine(rawLines, i) {
				continue
			}
		}

		if trimmed == "" {
			out = append(out, "")
			continue
		}

		// --- Horizontal rule ---
		if hrRe.MatchString(trimmed) {
			out = append(out, HrStyle.Render(strings.Repeat("\u2500", min(width, 40))))
			continue
		}

		// --- Blockquotes ---
		if strings.HasPrefix(trimmed, "> ") || trimmed == ">" {
			quoteText := strings.TrimPrefix(trimmed, "> ")
			quoteText = strings.TrimPrefix(quoteText, ">")
			wrapped := WrapWords(quoteText, width-4)
			for _, wl := range wrapped {
				out = append(out, BlockquoteStyle.Render("\u2502 ")+ApplyInlineFormatting(wl))
			}
			continue
		}

		// --- Headings ---
		if strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
			headingText := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			for _, wl := range WrapWords(headingText, width) {
				out = append(out, HeadingStyle.Render(ApplyInlineFormatting(wl)))
			}
			continue
		}

		// --- Bullet lists (supports nesting via indentation) ---
		if indent, item, ok := ParseBulletLine(line); ok {
			indentStr := strings.Repeat(" ", indent)
			wrapped := WrapWords(item, width-2-indent)
			if len(wrapped) > 0 {
				out = append(out, indentStr+BulletStyle.Render("\u2022 ")+ApplyInlineFormatting(wrapped[0]))
				contIndent := indentStr + "  "
				for j := 1; j < len(wrapped); j++ {
					out = append(out, contIndent+ApplyInlineFormatting(wrapped[j]))
				}
			}
			continue
		}

		// --- Numbered lists (supports indentation) ---
		if match := numberedListRe.FindStringSubmatch(line); match != nil {
			leadingSpaces := len(match[1])
			indentStr := strings.Repeat(" ", leadingSpaces)
			prefix := match[2] + ". "
			item := match[3]
			wrapped := WrapWords(item, width-len(prefix)-leadingSpaces)
			if len(wrapped) > 0 {
				out = append(out, indentStr+BulletStyle.Render(prefix)+ApplyInlineFormatting(wrapped[0]))
				contIndent := indentStr + strings.Repeat(" ", len(prefix))
				for j := 1; j < len(wrapped); j++ {
					out = append(out, contIndent+ApplyInlineFormatting(wrapped[j]))
				}
			}
			continue
		}

		// --- Regular paragraph text ---
		wrapped := WrapWords(line, width)
		if len(wrapped) == 0 {
			out = append(out, "")
			continue
		}
		for _, wl := range wrapped {
			out = append(out, ApplyInlineFormatting(wl))
		}
	}

	if inCode {
		out = append(out, renderHighlightedCodeBlock(codeLang, strings.Join(codeBuf, "\n"), width)...)
	}
	if inTable {
		flushTable()
	}

	return out
}

// isLastNonEmptyLine returns true if rawLines[i] is the last line that
// contains non-whitespace content.
func isLastNonEmptyLine(rawLines []string, i int) bool {
	for j := i + 1; j < len(rawLines); j++ {
		if strings.TrimSpace(rawLines[j]) != "" {
			return false
		}
	}
	return true
}

// ParseBulletLine detects a bullet list line (-, +, or *) with optional
// leading whitespace for nesting. Returns the indent level in spaces,
// the item text, and whether it matched.
func ParseBulletLine(line string) (indent int, item string, ok bool) {
	// Count leading whitespace.
	for _, ch := range line {
		if ch == ' ' {
			indent++
		} else if ch == '\t' {
			indent += 2
		} else {
			break
		}
	}
	rest := line[indent:]
	if strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "+ ") {
		return indent, strings.TrimSpace(rest[2:]), true
	}
	if strings.HasPrefix(rest, "* ") && !hrRe.MatchString(strings.TrimSpace(rest)) {
		return indent, strings.TrimSpace(rest[2:]), true
	}
	return 0, "", false
}

// ParseTableRow splits a pipe-delimited table row into trimmed cell strings.
func ParseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	// Strip leading and trailing pipes.
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// RenderTable renders a markdown table with box-drawing characters.
func RenderTable(headers []string, rows [][]string, width int) []string {
	numCols := len(headers)
	if numCols == 0 {
		return nil
	}

	const cellPad = 2 // one space each side of cell content

	// Calculate max content width for each column.
	colWidths := make([]int, numCols)
	for i, h := range headers {
		if w := stripMarkdownWidth(h); w > colWidths[i] {
			colWidths[i] = w
		}
	}
	for _, row := range rows {
		for i := 0; i < numCols && i < len(row); i++ {
			if w := stripMarkdownWidth(row[i]); w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// Fixed overhead: numCols+1 border chars + cellPad per column.
	fixedOverhead := numCols + 1 + numCols*cellPad
	available := width - fixedOverhead
	if available < numCols {
		available = numCols
	}

	// If total content width exceeds available, shrink proportionally.
	totalContent := 0
	for _, w := range colWidths {
		totalContent += w
	}
	if totalContent > available {
		for i := range colWidths {
			colWidths[i] = max(1, colWidths[i]*available/totalContent)
		}
	}

	// Build border lines.
	borderTop := buildBorder("\u250c", "\u252c", "\u2510", "\u2500", colWidths, cellPad)
	borderMid := buildBorder("\u251c", "\u253c", "\u2524", "\u2500", colWidths, cellPad)
	borderBot := buildBorder("\u2514", "\u2534", "\u2518", "\u2500", colWidths, cellPad)

	out := make([]string, 0, len(rows)+4)
	out = append(out, TableBorderStyle.Render(borderTop))

	// Header row.
	out = append(out, renderTableRow(headers, colWidths, cellPad, true))

	out = append(out, TableBorderStyle.Render(borderMid))

	// Data rows.
	for _, row := range rows {
		// Pad row to match column count.
		padded := make([]string, numCols)
		copy(padded, row)
		out = append(out, renderTableRow(padded, colWidths, cellPad, false))
	}

	out = append(out, TableBorderStyle.Render(borderBot))
	return out
}

// stripMarkdownWidth returns the visual width of text after stripping
// common inline markdown markers.
func stripMarkdownWidth(s string) int {
	s = inlineCodeRe.ReplaceAllString(s, "$1")
	s = boldRe.ReplaceAllString(s, "$1")
	s = strikethroughRe.ReplaceAllString(s, "$1")
	s = linkRe.ReplaceAllString(s, "$1 ($2)")
	return lipgloss.Width(s)
}

// buildBorder constructs a table border line using box-drawing characters.
func buildBorder(left, mid, right, horiz string, colWidths []int, cellPad int) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range colWidths {
		b.WriteString(strings.Repeat(horiz, w+cellPad))
		if i < len(colWidths)-1 {
			b.WriteString(mid)
		}
	}
	b.WriteString(right)
	return b.String()
}

// renderTableRow renders a single row of a table.
func renderTableRow(cells []string, colWidths []int, cellPad int, isHeader bool) string {
	var b strings.Builder
	b.WriteString(TableBorderStyle.Render("\u2502"))
	for i, w := range colWidths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}

		// Format the cell first, then measure and pad based on styled width.
		var styled string
		if isHeader {
			// Strip bold markers from headers since the style already applies bold.
			clean := boldRe.ReplaceAllString(cell, "$1")
			styled = TableHeaderStyle.Render(clean)
		} else {
			styled = ApplyInlineFormatting(cell)
		}

		styledWidth := lipgloss.Width(styled)

		// Truncate if styled content exceeds column width.
		if styledWidth > w {
			// Re-format from a truncated raw cell.
			raw := boldRe.ReplaceAllString(cell, "$1")
			raw = inlineCodeRe.ReplaceAllString(raw, "$1")
			raw = strikethroughRe.ReplaceAllString(raw, "$1")
			raw = linkRe.ReplaceAllString(raw, "$1 ($2)")
			raw = TruncateToWidth(raw, w)
			if isHeader {
				styled = TableHeaderStyle.Render(raw)
			} else {
				styled = raw
			}
			styledWidth = lipgloss.Width(styled)
		}

		padRight := w - styledWidth
		if padRight < 0 {
			padRight = 0
		}
		b.WriteString(" " + styled + strings.Repeat(" ", padRight) + " ")
		if i < len(colWidths)-1 {
			b.WriteString(TableBorderStyle.Render("\u2502"))
		}
	}
	b.WriteString(TableBorderStyle.Render("\u2502"))
	return b.String()
}

// TruncateToWidth truncates s to fit within maxWidth visible columns,
// handling multi-byte characters safely.
func TruncateToWidth(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i])
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return ""
}

// renderHighlightedCodeBlock syntax-highlights a fenced code block using
// Chroma and prepends subtle line numbers with a gutter.
func renderHighlightedCodeBlock(lang string, code string, width int) []string {
	if width < 20 {
		width = 20
	}
	if lang == "" || lang == "text" {
		lang = "plaintext"
	}

	var highlighted bytes.Buffer
	if err := quick.Highlight(&highlighted, code, lang, "terminal256", "dracula"); err != nil {
		highlighted.Reset()
		if err := quick.Highlight(&highlighted, code, "plaintext", "terminal256", "dracula"); err != nil {
			// plaintext highlight fallback; nothing further to do
		}
	}
	hlLines := strings.Split(strings.TrimSuffix(highlighted.String(), "\n"), "\n")
	if len(hlLines) == 0 {
		hlLines = []string{""}
	}

	out := make([]string, 0, len(hlLines))
	for i, line := range hlLines {
		lineNo := CodeGutterStyle.Render(fmt.Sprintf("%3d", i+1))
		gutter := CodeGutterStyle.Render(" \u2502 ")
		out = append(out, lineNo+gutter+line)
	}
	return out
}

// ApplyInlineFormatting handles inline markdown: `code`, [text](url),
// **bold**, *italic*, and ~~strikethrough~~.
// Should not be applied to code block lines.
func ApplyInlineFormatting(s string) string {
	// Inline code first -- protect contents from further processing.
	s = inlineCodeRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := inlineCodeRe.FindStringSubmatch(match)[1]
		return InlineCodeStyle.Render(inner)
	})

	// Links: [text](url) -> text (url)
	s = linkRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		return LinkTextStyle.Render(parts[1]) + LinkURLStyle.Render(" ("+parts[2]+")")
	})

	// Strikethrough: ~~text~~
	s = strikethroughRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := strikethroughRe.FindStringSubmatch(match)[1]
		return StrikethroughStyle.Render(inner)
	})

	// Bold: **text** (must come before italic to avoid conflict)
	s = boldRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := boldRe.FindStringSubmatch(match)[1]
		return BoldInlineStyle.Render(inner)
	})

	// Italic: *text*
	s = ApplyItalic(s)

	return s
}

// ApplyItalic handles *italic* markers that weren't consumed by bold.
// It manually scans for single * delimiters that aren't adjacent to other *s.
func ApplyItalic(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		// Skip ANSI escape sequences (from already-styled content).
		if s[i] == '\x1b' {
			j := i + 1
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++ // include the 'm'
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}

		if s[i] != '*' {
			b.WriteByte(s[i])
			i++
			continue
		}

		// Found a *. Check it's not ** (bold already handled).
		if i+1 < len(s) && s[i+1] == '*' {
			b.WriteByte(s[i])
			i++
			continue
		}

		// Look for the closing *.
		end := strings.Index(s[i+1:], "*")
		if end < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		end += i + 1 // absolute position of closing *

		// Make sure closing * isn't part of ** either.
		if end+1 < len(s) && s[end+1] == '*' {
			b.WriteByte(s[i])
			i++
			continue
		}

		inner := s[i+1 : end]
		if len(inner) == 0 {
			b.WriteByte(s[i])
			i++
			continue
		}

		b.WriteString(ItalicInlineStyle.Render(inner))
		i = end + 1
	}
	return b.String()
}

// FindSafeFlushPoint returns the byte length of unflushed content that is safe
// to push to scrollback. It returns 0 if nothing is ready yet.
func FindSafeFlushPoint(s string) int {
	// Need at least 10 newlines in the unflushed portion.
	nlCount := 0
	for _, ch := range s {
		if ch == '\n' {
			nlCount++
		}
	}
	if nlCount < 10 {
		return 0
	}

	// Find the last paragraph boundary (\n\n).
	splitIdx := strings.LastIndex(s, "\n\n")
	if splitIdx < 0 {
		return 0
	}
	// Include the double newline in the flushed prefix.
	candidate := splitIdx + 2

	// Count code fences in the candidate prefix.
	prefix := s[:candidate]
	fenceCount := strings.Count(prefix, "```")
	if fenceCount%2 == 0 {
		return candidate
	}

	// Odd fence count means we're inside a code block.
	// Back up to the paragraph boundary before the last "```".
	lastFence := strings.LastIndex(prefix, "```")
	if lastFence < 0 {
		return 0
	}
	// Find paragraph boundary before that fence.
	backup := strings.LastIndex(prefix[:lastFence], "\n\n")
	if backup < 0 {
		return 0
	}
	backupCandidate := backup + 2

	// Verify the backed-up prefix has balanced fences.
	backupPrefix := s[:backupCandidate]
	if strings.Count(backupPrefix, "```")%2 != 0 {
		return 0
	}
	return backupCandidate
}

// FormatBlockMessage renders a transcript message that may contain structured
// content blocks. Falls back to FormatMessageForScrollback for plain messages.
func FormatBlockMessage(msg domain.TranscriptMessage, width int) string {
	if !msg.HasBlocks() {
		return FormatMessageForScrollback(msg, width)
	}

	contentWidth := max(20, width-4)
	var b strings.Builder

	switch msg.Role {
	case "assistant":
		first := true
		for _, block := range msg.Blocks {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) == "" {
					continue
				}
				textMsg := domain.TranscriptMessage{Role: "assistant", Content: block.Text}
				if !first {
					b.WriteString("\n")
				}
				b.WriteString(FormatMessageForScrollback(textMsg, width))
				first = false
			case "tool_use":
				if !first {
					b.WriteString("\n")
				}
				b.WriteString(FormatToolUse(block, contentWidth))
				first = false
			}
		}
		if first {
			return AsstIconStyle.Render("\u25cf ") + "(no text)"
		}

	case "user":
		firstResult := true
		for _, block := range msg.Blocks {
			if block.Type == "tool_result" {
				if !firstResult {
					b.WriteString("\n")
				}
				b.WriteString(FormatToolResult(block.ToolName, block.ToolResult, block.IsError, contentWidth))
				firstResult = false
			}
		}

	default:
		return FormatMessageForScrollback(msg, width)
	}

	return b.String()
}

// SortedToolParams returns parameter keys in a deterministic order,
// placing the primary param for each tool first.
func SortedToolParams(toolName string, input map[string]any) []string {
	primary := map[string]string{
		"file_read":  "path",
		"file_write": "path",
		"file_edit":  "path",
		"bash":       "command",
		"grep":       "pattern",
		"list_files": "path",
	}

	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Move primary key to front if it exists.
	if pk, ok := primary[toolName]; ok {
		for i, k := range keys {
			if k == pk {
				keys = append([]string{k}, append(keys[:i], keys[i+1:]...)...)
				break
			}
		}
	}
	return keys
}

// TruncateParam returns a truncated string representation of a tool param.
func TruncateParam(key string, val any) string {
	valStr := fmt.Sprintf("%v", val)
	limit := 50
	switch key {
	case "command":
		limit = 80
	case "path", "old_string", "new_string", "content":
		limit = 200
	}
	if len(valStr) > limit {
		valStr = valStr[:limit] + "..."
	}
	return valStr
}

// FormatToolUse renders a tool_use block as a styled header with sorted params.
func FormatToolUse(block domain.ContentBlock, width int) string {
	keys := SortedToolParams(block.ToolName, block.ToolInput)
	params := make([]string, 0, len(keys))
	for _, k := range keys {
		params = append(params, k+"="+TruncateParam(k, block.ToolInput[k]))
	}

	header := ToolNameStyle.Render("[tool] " + block.ToolName)
	if len(params) > 0 {
		header += ToolInputStyle.Render("(" + strings.Join(params, ", ") + ")")
	}
	return header
}

// ToolResultHeader returns a brief, tool-specific summary header.
func ToolResultHeader(toolName, result string) string {
	switch toolName {
	case "file_read":
		n := strings.Count(result, "\n")
		return fmt.Sprintf("[read] %d lines", n)
	case "file_write":
		return "[write] " + result
	case "file_edit":
		return "[edit] " + result
	case "bash":
		n := strings.Count(result, "\n")
		return fmt.Sprintf("[bash] %d lines of output", n)
	case "grep":
		if result == "No matches found." {
			return "[grep] 0 matches"
		}
		n := 0
		for _, line := range strings.Split(result, "\n") {
			if line != "--" && !strings.HasPrefix(line, "...") {
				n++
			}
		}
		return fmt.Sprintf("[grep] %d matches", n)
	case "list_files":
		if result == "No entries found." {
			return "[files] 0 entries"
		}
		n := strings.Count(result, "\n") + 1
		return fmt.Sprintf("[files] %d entries", n)
	default:
		return "[result] " + toolName
	}
}

// FormatToolResult renders a tool result or error.
func FormatToolResult(toolName, result string, isError bool, width int) string {
	style := ToolResultStyle
	var label string
	if isError {
		label = "[error] " + toolName
		style = ErrorLineStyle
	} else {
		label = ToolResultHeader(toolName, result)
	}

	lines := strings.Split(result, "\n")
	if len(lines) > 20 {
		lines = append(lines[:20], fmt.Sprintf("... (%d more lines)", len(lines)-20))
	}

	truncated := strings.Join(lines, "\n")
	if len(truncated) > 2000 {
		truncated = truncated[:2000] + "\n... (truncated)"
	}

	header := style.Render(label)
	if strings.TrimSpace(truncated) == "" {
		return header
	}
	return header + "\n" + ToolInputStyle.Render(truncated)
}

// FormatMessageForScrollback renders a single transcript message into a
// styled string ready to be printed into the terminal scrollback.
func FormatMessageForScrollback(msg domain.TranscriptMessage, width int) string {
	contentWidth := max(20, width-4)

	switch msg.Role {
	case "user":
		wrapped := WrapWords(msg.Content, contentWidth-2)
		if len(wrapped) == 0 {
			return UserIconStyle.Render("\u25cf ")
		}
		var b strings.Builder
		b.WriteString(UserIconStyle.Render("\u25cf ") + wrapped[0])
		for i := 1; i < len(wrapped); i++ {
			b.WriteString("\n  " + wrapped[i])
		}
		return b.String()

	case "assistant":
		lines := RenderAssistantLines(msg.Content, contentWidth-2)
		if len(lines) == 0 {
			return AsstIconStyle.Render("\u25cf ")
		}
		var b strings.Builder
		first := lines[0]
		if strings.HasPrefix(first, "Error:") {
			first = ErrorLineStyle.Render(first)
		}
		b.WriteString(AsstIconStyle.Render("\u25cf ") + first)
		for i := 1; i < len(lines); i++ {
			line := lines[i]
			if strings.HasPrefix(line, "Error:") {
				line = ErrorLineStyle.Render(line)
			}
			b.WriteString("\n  " + line)
		}
		return b.String()

	default:
		wrapped := WrapWords(msg.Content, contentWidth)
		var b strings.Builder
		for i, line := range wrapped {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(WelcomeStyle.Render(line))
		}
		return b.String()
	}
}
