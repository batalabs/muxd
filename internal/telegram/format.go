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

	// Rewrite markdown tables into mobile-friendly row cards.
	text = rewriteMarkdownTablesForMobile(text)

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

	// Blockquotes: > text -> italic with quote marker
	text = blockquoteRegex.ReplaceAllString(text, "  <i>$1</i>")

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

	// TableRowRegex: line starting with optional whitespace then |
	TableRowRegex = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	tableSepRegex = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$`)
)

func rewriteMarkdownTablesForMobile(text string) string {
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
			out = append(out, formatMobileTable(headers, rows)...)
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

func formatMobileTable(headers []string, rows [][]string) []string {
	if len(headers) == 0 {
		return nil
	}
	var out []string
	out = append(out, fmt.Sprintf("Table (%d rows):", len(rows)))
	for i, row := range rows {
		out = append(out, fmt.Sprintf("%d)", i+1))
		for j, h := range headers {
			if h == "" {
				h = fmt.Sprintf("col%d", j+1)
			}
			val := ""
			if j < len(row) {
				val = row[j]
			}
			out = append(out, fmt.Sprintf("  %s: %s", h, val))
		}
		if i < len(rows)-1 {
			out = append(out, "")
		}
	}
	return out
}

// ProtectTables finds consecutive lines that look like markdown table rows
// (lines containing | delimiters) and replaces them with a code block
// placeholder so they render as monospace <pre> in Telegram.
func ProtectTables(text string, codeBlocks *[]codeRegion) string {
	lines := strings.Split(text, "\n")
	var result []string
	var tableLines []string

	flushTable := func() {
		if len(tableLines) == 0 {
			return
		}
		tableText := strings.Join(tableLines, "\n")
		placeholder := fmt.Sprintf("\x00CODEBLOCK%d\x00", len(*codeBlocks))
		*codeBlocks = append(*codeBlocks, codeRegion{code: tableText})
		result = append(result, placeholder)
		tableLines = nil
	}

	for _, line := range lines {
		if TableRowRegex.MatchString(line) {
			tableLines = append(tableLines, line)
		} else {
			flushTable()
			result = append(result, line)
		}
	}
	flushTable()

	return strings.Join(result, "\n")
}
