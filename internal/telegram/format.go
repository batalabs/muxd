package telegram

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// MarkdownToTelegramHTML converts common markdown patterns found in LLM output
// to Telegram-compatible HTML. Telegram's HTML mode supports a limited subset:
// <b>, <i>, <s>, <code>, <pre>, <a href="...">.
//
// The conversion protects code blocks and inline code first (so their contents
// aren't processed as markdown), escapes HTML entities in the remaining text,
// converts markdown patterns, then restores code regions with proper tags.
func MarkdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	// Phase 1a: Extract and protect fenced code blocks (```lang\n...\n```)
	var codeBlocks []codeRegion
	text = fencedCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := fencedCodeRegex.FindStringSubmatch(match)
		lang := ""
		code := match
		if len(sub) >= 3 {
			lang = strings.TrimSpace(sub[1])
			code = sub[2]
		}
		placeholder := fmt.Sprintf("\x00CODEBLOCK%d\x00", len(codeBlocks))
		codeBlocks = append(codeBlocks, codeRegion{lang: lang, code: code})
		return placeholder
	})

	// Rewrite markdown tables into aligned <pre> blocks.
	text = rewriteMarkdownTables(text, &codeBlocks)

	// Phase 2: Extract and protect inline code (`...`)
	var inlineCodes []string
	text = inlineCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Strip the surrounding backticks
		inner := match[1 : len(match)-1]
		placeholder := fmt.Sprintf("\x00INLINECODE%d\x00", len(inlineCodes))
		inlineCodes = append(inlineCodes, inner)
		return placeholder
	})

	// Phase 3: Escape HTML entities in the remaining (non-code) text
	text = EscapeHTML(text)

	// Phase 4: Convert markdown patterns to HTML

	// Headers: lines starting with # (up to 6 levels) -> bold
	text = headerRegex.ReplaceAllString(text, "<b>$1</b>")

	// Horizontal rules: --- / *** / ___ -> visual separator (before bold/italic)
	text = hrRegex.ReplaceAllString(text, "————————————————")

	// Bold: **text** -> <b>text</b>
	text = boldRegex.ReplaceAllString(text, "<b>$1</b>")
	// Bold: __text__ -> <b>text</b>
	text = boldUnderscoreRegex.ReplaceAllString(text, "<b>$1</b>")

	// Italic: *text* (not preceded/followed by *) -> <i>text</i>
	// Must run after bold so ** is consumed first.
	text = italicRegex.ReplaceAllString(text, "<i>$1</i>")
	// Italic: _text_ -> <i>text</i>
	text = italicUnderscoreRegex.ReplaceAllString(text, "$1<i>$2</i>")

	// Strikethrough: ~~text~~ -> <s>text</s>
	text = strikethroughRegex.ReplaceAllString(text, "<s>$1</s>")

	// Links: [text](url) -> <a href="url">text</a>
	text = linkRegex.ReplaceAllStringFunc(text, func(match string) string {
		sub := linkRegex.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, EscapeHTMLAttr(html.UnescapeString(sub[2])), sub[1])
	})

	// Blockquotes: > text -> "▎ text" with italic
	text = blockquoteRegex.ReplaceAllString(text, "▎ <i>$1</i>")

	// Unordered lists: - item / * item -> • item
	text = ulRegex.ReplaceAllString(text, "${1}• ")

	// Phase 5: Restore code blocks with <pre><code> tags
	for i, block := range codeBlocks {
		placeholder := fmt.Sprintf("\x00CODEBLOCK%d\x00", i)
		escaped := EscapeHTML(block.code)
		replacement := fmt.Sprintf("<pre><code>%s</code></pre>", escaped)
		text = strings.Replace(text, placeholder, replacement, 1)
	}

	// Phase 6: Restore inline code with <code> tags
	for i, code := range inlineCodes {
		placeholder := fmt.Sprintf("\x00INLINECODE%d\x00", i)
		escaped := EscapeHTML(code)
		replacement := fmt.Sprintf("<code>%s</code>", escaped)
		text = strings.Replace(text, placeholder, replacement, 1)
	}

	return text
}

// codeRegion holds a fenced code block's language tag and raw content.
type codeRegion struct {
	lang string
	code string
}

// EscapeHTML escapes characters that are special in Telegram's HTML parse mode.
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// EscapeHTMLAttr escapes characters that are special in HTML attribute values.
func EscapeHTMLAttr(s string) string {
	s = EscapeHTML(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, `'`, "&#39;")
	return s
}

// Compiled regexes for markdown pattern matching.
var (
	// Fenced code blocks: ```lang\ncode\n``` (lang is optional).
	// (?s) makes . match newlines inside the code block.
	fencedCodeRegex = regexp.MustCompile("(?s)```([^`\n]*)\\n(.*?)\\n?```")

	// Inline code: `code` (no newlines allowed inside).
	inlineCodeRegex = regexp.MustCompile("`([^`\n]+)`")

	// Headers: lines starting with 1-6 # characters.
	headerRegex = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

	// Bold: **text** (non-greedy, no newlines).
	boldRegex = regexp.MustCompile(`\*\*(.+?)\*\*`)
	// Bold: __text__ (non-greedy, no newlines).
	boldUnderscoreRegex = regexp.MustCompile(`__([^_\n]+?)__`)

	// Italic: *text* -- after bold is consumed, remaining single-asterisk pairs are italic.
	italicRegex = regexp.MustCompile(`\*([^*\n]+)\*`)
	// Italic: _text_ (avoid grabbing within words, e.g. snake_case).
	italicUnderscoreRegex = regexp.MustCompile(`(^|[^[:alnum:]_])_([^_\n]+)_`)

	// Strikethrough: ~~text~~.
	strikethroughRegex = regexp.MustCompile(`~~(.+?)~~`)

	// Links: [text](url).
	linkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// Blockquotes: lines starting with >
	blockquoteRegex = regexp.MustCompile(`(?m)^&gt;\s?(.+)$`)

	// Horizontal rules: ---, ***, ___ (3 or more, alone on a line)
	hrRegex = regexp.MustCompile(`(?m)^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)

	// Unordered list items: lines starting with - or * followed by space
	ulRegex = regexp.MustCompile(`(?m)^(\s*)[-*]\s+`)

	// TableRowRegex: line starting with optional whitespace then |
	TableRowRegex = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	tableSepRegex = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$`)
)

// rewriteMarkdownTables detects markdown tables and converts them into
// code-block placeholders so they render as aligned <pre> blocks in Telegram.
func rewriteMarkdownTables(text string, codeBlocks *[]codeRegion) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if TableRowRegex.MatchString(line) && i+1 < len(lines) && tableSepRegex.MatchString(strings.TrimSpace(lines[i+1])) {
			headers := parseMarkdownTableRow(line)
			i += 2 // skip header + separator
			var rows [][]string
			for i < len(lines) && TableRowRegex.MatchString(strings.TrimSpace(lines[i])) {
				rows = append(rows, parseMarkdownTableRow(strings.TrimSpace(lines[i])))
				i++
			}
			tableText := formatAlignedTable(headers, rows)
			placeholder := fmt.Sprintf("\x00CODEBLOCK%d\x00", len(*codeBlocks))
			*codeBlocks = append(*codeBlocks, codeRegion{code: tableText})
			out = append(out, placeholder)
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return strings.Join(out, "\n")
}

func parseMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// formatAlignedTable renders headers and rows as a preformatted code block
// with padded columns so Telegram displays a clean, aligned table.
func formatAlignedTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	// Compute column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		if len(h) > widths[i] {
			widths[i] = len(h)
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Cap columns at 30 chars to keep it readable on mobile.
	for i := range widths {
		if widths[i] > 30 {
			widths[i] = 30
		}
	}

	padCell := func(s string, w int) string {
		if len(s) > w {
			if w > 1 {
				return s[:w-1] + "~"
			}
			return s[:w]
		}
		return s + strings.Repeat(" ", w-len(s))
	}

	formatRow := func(cells []string) string {
		parts := make([]string, len(headers))
		for i := range headers {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts[i] = padCell(cell, widths[i])
		}
		return strings.Join(parts, " │ ")
	}

	var sb strings.Builder
	// Header row.
	sb.WriteString(formatRow(headers))
	sb.WriteByte('\n')
	// Separator.
	seps := make([]string, len(headers))
	for i, w := range widths {
		seps[i] = strings.Repeat("─", w)
	}
	sb.WriteString(strings.Join(seps, "─┼─"))
	sb.WriteByte('\n')
	// Data rows.
	for i, row := range rows {
		sb.WriteString(formatRow(row))
		if i < len(rows)-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
