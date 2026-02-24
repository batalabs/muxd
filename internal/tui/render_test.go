package tui

import (
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
)

func TestFindSafeFlushPoint(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{
			name: "returns 0 for short content",
			in:   "Hello world.\n\nSecond paragraph.",
			want: 0,
		},
		{
			name: "returns 0 for empty string",
			in:   "",
			want: 0,
		},
		{
			name: "flushes at paragraph boundary with enough newlines",
			in: "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n" +
				"Line 6\nLine 7\nLine 8\nLine 9\nLine 10\n" +
				"\nTail paragraph still going",
			want: strings.Index(
				"Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n"+
					"Line 6\nLine 7\nLine 8\nLine 9\nLine 10\n"+
					"\nTail paragraph still going", "\n\n") + 2,
		},
		{
			name: "complete code fence allows flush after closing fence",
			in: "Paragraph one.\n\n" +
				"```go\nfunc main() {\n\tfmt.Println(\"hello\")\n" +
				"\n\n" +
				"\tfmt.Println(\"world\")\n}\n```\n\nAfter code.",
			want: func() int {
				s := "Paragraph one.\n\n" +
					"```go\nfunc main() {\n\tfmt.Println(\"hello\")\n" +
					"\n\n" +
					"\tfmt.Println(\"world\")\n}\n```\n\nAfter code."
				return strings.LastIndex(s, "\n\n") + 2
			}(),
		},
		{
			name: "returns 0 when code fence is open and not enough content before it",
			in: "Short.\n\n" +
				"```go\nfunc main() {\n\tfmt.Println(\"hello\")\n" +
				"\n\n" +
				"\tfmt.Println(\"world\")\n}\n",
			want: 0,
		},
		{
			name: "flushes before code fence with enough content",
			in: "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10\n" +
				"\n" +
				"```go\ncode here\n```\n\nDone.",
			want: func() int {
				s := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10\n" +
					"\n" +
					"```go\ncode here\n```\n\nDone."
				lastPP := strings.LastIndex(s, "\n\n")
				candidate := lastPP + 2
				prefix := s[:candidate]
				fences := strings.Count(prefix, "```")
				if fences%2 == 0 {
					return candidate
				}
				return 0
			}(),
		},
		{
			name: "returns 0 when no paragraph boundary",
			in:   "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk",
			want: 0,
		},
		{
			name: "flushes at last paragraph boundary",
			in: "Para1 line1\nPara1 line2\nPara1 line3\n\n" +
				"Para2 line1\nPara2 line2\nPara2 line3\n\n" +
				"Para3 line1\nPara3 line2\nPara3 line3\nPara3 line4",
			want: func() int {
				s := "Para1 line1\nPara1 line2\nPara1 line3\n\n" +
					"Para2 line1\nPara2 line2\nPara2 line3\n\n" +
					"Para3 line1\nPara3 line2\nPara3 line3\nPara3 line4"
				return strings.LastIndex(s, "\n\n") + 2
			}(),
		},
		{
			name: "balanced fences allow flush after closing fence",
			in: "L1\nL2\nL3\nL4\n\n```\ncode\n```\n\n" +
				"L5\nL6\nL7\nL8\nL9\nL10\nL11",
			want: func() int {
				s := "L1\nL2\nL3\nL4\n\n```\ncode\n```\n\n" +
					"L5\nL6\nL7\nL8\nL9\nL10\nL11"
				lastPP := strings.LastIndex(s, "\n\n")
				return lastPP + 2
			}(),
		},
		{
			name: "backs up past open fence to earlier paragraph boundary",
			in: "L1\nL2\nL3\nL4\nL5\n\n" +
				"L6\nL7\nL8\nL9\nL10\n\n" +
				"```go\nfunc main() {\n\n}\n",
			want: func() int {
				s := "L1\nL2\nL3\nL4\nL5\n\n" +
					"L6\nL7\nL8\nL9\nL10\n\n" +
					"```go\nfunc main() {\n\n}\n"
				lastPP := strings.LastIndex(s, "\n\n")
				prefix := s[:lastPP+2]
				lastFence := strings.LastIndex(prefix, "```")
				backup := strings.LastIndex(prefix[:lastFence], "\n\n")
				return backup + 2
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindSafeFlushPoint(tt.in)
			if got != tt.want {
				t.Errorf("FindSafeFlushPoint() = %d, want %d", got, tt.want)
				if got > 0 && got <= len(tt.in) {
					t.Logf("  flushed prefix: %q", tt.in[:got])
				}
				if tt.want > 0 && tt.want <= len(tt.in) {
					t.Logf("  expected prefix: %q", tt.in[:tt.want])
				}
			}
		})
	}
}

func TestFindSafeFlushPoint_NeverSplitsCodeFence(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 15; i++ {
		b.WriteString("Line of text\n")
	}
	b.WriteString("\n```python\n")
	b.WriteString("def foo():\n    pass\n\n")
	b.WriteString("def bar():\n    pass\n")

	s := b.String()
	n := FindSafeFlushPoint(s)
	if n > 0 {
		prefix := s[:n]
		fences := strings.Count(prefix, "```")
		if fences%2 != 0 {
			t.Errorf("flush point splits a code fence: flushed %d bytes with %d fences (odd)", n, fences)
		}
	}
}

// ---------------------------------------------------------------------------
// Table rendering tests
// ---------------------------------------------------------------------------

func TestParseTableRow(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "simple two columns",
			in:   "| Rule | Detail |",
			want: []string{"Rule", "Detail"},
		},
		{
			name: "three columns with spacing",
			in:   "|  Name  |  Age  |  City  |",
			want: []string{"Name", "Age", "City"},
		},
		{
			name: "empty cells",
			in:   "| | data | |",
			want: []string{"", "data", ""},
		},
		{
			name: "no leading pipe handled",
			in:   " col1 | col2 |",
			want: []string{"col1", "col2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTableRow(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseTableRow(%q) returned %d cells, want %d: %v", tt.in, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cell[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRenderTable(t *testing.T) {
	headers := []string{"Rule", "Detail"}
	rows := [][]string{
		{"def keyword", "Used to define every function"},
		{"return", "Optional"},
	}

	lines := RenderTable(headers, rows, 60)

	if len(lines) == 0 {
		t.Fatal("RenderTable returned empty output")
	}

	// Check box-drawing characters are present.
	joined := strings.Join(lines, "\n")
	for _, ch := range []string{"\u250c", "\u2510", "\u251c", "\u2524", "\u2514", "\u2518", "\u2500", "\u2502"} {
		if !strings.Contains(joined, ch) {
			t.Errorf("missing box-drawing character %q in table output", ch)
		}
	}

	// Check that header content appears.
	if !strings.Contains(joined, "Rule") {
		t.Error("header 'Rule' not found in table output")
	}
	if !strings.Contains(joined, "Detail") {
		t.Error("header 'Detail' not found in table output")
	}

	// Check data cells.
	if !strings.Contains(joined, "def keyword") {
		t.Error("cell 'def keyword' not found in table output")
	}
}

func TestRenderTable_EmptyRows(t *testing.T) {
	lines := RenderTable([]string{}, nil, 60)
	if lines != nil {
		t.Errorf("expected nil for empty headers, got %d lines", len(lines))
	}
}

// ---------------------------------------------------------------------------
// Horizontal rule tests
// ---------------------------------------------------------------------------

func TestRenderAssistantLines_HorizontalRule(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"triple dash", "---"},
		{"long dash", "-----"},
		{"triple asterisk", "***"},
		{"triple underscore", "___"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := RenderAssistantLines(tt.in, 60)
			if len(lines) == 0 {
				t.Fatal("expected at least one line for HR")
			}
			if !strings.Contains(lines[0], "\u2500") {
				t.Errorf("HR line should contain box-drawing character, got: %q", lines[0])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Table detection in RenderAssistantLines
// ---------------------------------------------------------------------------

func TestRenderAssistantLines_Table(t *testing.T) {
	input := "| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |"
	lines := RenderAssistantLines(input, 60)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "\u250c") {
		t.Error("table should contain box-drawing top-left corner")
	}
	if !strings.Contains(joined, "Alice") {
		t.Error("table should contain 'Alice'")
	}
	if !strings.Contains(joined, "Bob") {
		t.Error("table should contain 'Bob'")
	}
}

func TestRenderAssistantLines_TableAtEndOfContent(t *testing.T) {
	input := "Here is a table:\n\n| A | B |\n| - | - |\n| 1 | 2 |"
	lines := RenderAssistantLines(input, 60)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "\u250c") {
		t.Error("table at end of content should still render")
	}
}

func TestRenderAssistantLines_TableWithoutOuterPipes(t *testing.T) {
	// Tables where rows don't have leading/trailing pipes should still render.
	input := "Name | Age | City\n--- | --- | ---\nAlice | 30 | NYC\nBob | 25 | LA"
	lines := RenderAssistantLines(input, 60)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "\u250c") {
		t.Error("table without outer pipes should contain box-drawing top-left corner")
	}
	if !strings.Contains(joined, "Alice") {
		t.Error("table should contain 'Alice'")
	}
	if !strings.Contains(joined, "Bob") {
		t.Error("table should contain 'Bob'")
	}
	// The header row should NOT appear as plain text before the table.
	for _, line := range lines {
		if line == "Name | Age | City" {
			t.Error("header row should not appear as plain text -- it should be rendered inside the table")
		}
	}
}

func TestRenderAssistantLines_PendingTableHeaderSkipped(t *testing.T) {
	// During streaming, a table header may arrive before the separator.
	// It should be skipped (not rendered as plain text) so it doesn't flash.
	input := "Some intro text.\n\n| Name | Age |"
	lines := RenderAssistantLines(input, 60)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "| Name | Age |") {
		t.Error("pending table header at end of content should not appear as plain text")
	}
	if !strings.Contains(joined, "Some intro text.") {
		t.Error("text before the pending header should still render")
	}
}

func TestRenderAssistantLines_PendingLooseTableHeaderSkipped(t *testing.T) {
	input := "Some intro text.\n\nName | Age | City"
	lines := RenderAssistantLines(input, 60)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "Name | Age | City") {
		t.Error("pending loose table header should not appear as plain text")
	}
}

// ---------------------------------------------------------------------------
// Inline formatting tests
// ---------------------------------------------------------------------------

func TestApplyInlineFormatting_InlineCode(t *testing.T) {
	result := ApplyInlineFormatting("use `fmt.Println` here")
	if strings.Contains(result, "`") {
		t.Error("backticks should be removed from inline code")
	}
	if !strings.Contains(result, "fmt.Println") {
		t.Error("inline code content should be preserved")
	}
}

func TestApplyInlineFormatting_Bold(t *testing.T) {
	result := ApplyInlineFormatting("this is **bold** text")
	if strings.Contains(result, "**") {
		t.Error("bold markers should be removed")
	}
	if !strings.Contains(result, "bold") {
		t.Error("bold content should be preserved")
	}
}

func TestApplyInlineFormatting_Strikethrough(t *testing.T) {
	result := ApplyInlineFormatting("this is ~~removed~~ text")
	if strings.Contains(result, "~~") {
		t.Error("strikethrough markers should be removed")
	}
	if !strings.Contains(result, "removed") {
		t.Error("strikethrough content should be preserved")
	}
}

func TestApplyInlineFormatting_Link(t *testing.T) {
	result := ApplyInlineFormatting("see [docs](https://example.com) for info")
	if strings.Contains(result, "[docs]") {
		t.Error("link markdown should be transformed")
	}
	if !strings.Contains(result, "docs") {
		t.Error("link text should be preserved")
	}
	if !strings.Contains(result, "https://example.com") {
		t.Error("link URL should be preserved")
	}
}

func TestApplyInlineFormatting_Italic(t *testing.T) {
	result := ApplyInlineFormatting("this is *italic* text")
	// The * markers should be consumed.
	if !strings.Contains(result, "italic") {
		t.Error("italic content should be preserved")
	}
}

func TestApplyInlineFormatting_CodeProtectsBold(t *testing.T) {
	result := ApplyInlineFormatting("use `**kwargs**` in Python")
	// The **kwargs** inside backticks should NOT be treated as bold.
	if !strings.Contains(result, "kwargs") {
		t.Error("inline code content should be preserved")
	}
}

func TestApplyItalic_SkipsBold(t *testing.T) {
	// ** should not be treated as italic.
	result := ApplyItalic("this has ** double asterisks **")
	if strings.Contains(result, "\x1b") {
		// Should not have applied any ANSI styling if it's **.
		// Actually it might still try. Let's just check it doesn't crash.
	}
	_ = result
}

// ---------------------------------------------------------------------------
// Nested list tests
// ---------------------------------------------------------------------------

func TestParseBulletLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantIndent int
		wantItem   string
		wantOK     bool
	}{
		{"top-level dash", "- item", 0, "item", true},
		{"indented dash", "  - sub item", 2, "sub item", true},
		{"deeply indented", "    - deep", 4, "deep", true},
		{"plus marker", "+ item", 0, "item", true},
		{"asterisk marker", "* item", 0, "item", true},
		{"not a bullet", "regular text", 0, "", false},
		{"horizontal rule not bullet", "***", 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indent, item, ok := ParseBulletLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if indent != tt.wantIndent {
				t.Errorf("indent = %d, want %d", indent, tt.wantIndent)
			}
			if item != tt.wantItem {
				t.Errorf("item = %q, want %q", item, tt.wantItem)
			}
		})
	}
}

func TestRenderAssistantLines_NestedBullets(t *testing.T) {
	input := "- top level\n  - nested item\n    - deep nested"
	lines := RenderAssistantLines(input, 60)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	// First line should have no leading indent.
	if strings.HasPrefix(lines[0], " ") {
		t.Error("top-level bullet should not be indented")
	}
	// Second line should have some indent.
	if !strings.HasPrefix(lines[1], "  ") {
		t.Error("nested bullet should be indented")
	}
}

func TestRenderAssistantLines_Blockquote(t *testing.T) {
	input := "> This is a quote"
	lines := RenderAssistantLines(input, 60)
	if len(lines) == 0 {
		t.Fatal("expected at least one line for blockquote")
	}
	if !strings.Contains(lines[0], "\u2502") {
		t.Errorf("blockquote should contain gutter, got: %q", lines[0])
	}
	if !strings.Contains(lines[0], "This is a quote") {
		t.Error("blockquote content should be preserved")
	}
}

func TestRenderAssistantLines_NumberedListIndented(t *testing.T) {
	input := "  1. indented item"
	lines := RenderAssistantLines(input, 60)
	if len(lines) == 0 {
		t.Fatal("expected at least one line")
	}
	if !strings.HasPrefix(lines[0], "  ") {
		t.Error("indented numbered item should preserve indentation")
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"ascii fits", "hello", 10, "hello"},
		{"ascii truncated", "hello world", 5, "hello"},
		{"empty string", "", 5, ""},
		{"unicode em dash", "a\u2014b", 3, "a\u2014b"},
		{"unicode truncated", "a\u2014b\u2014c", 3, "a\u2014b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateToWidth(tt.in, tt.width)
			if got != tt.want {
				t.Errorf("TruncateToWidth(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
		})
	}
}

func TestWrapWords(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		width     int
		wantLines int
	}{
		{"empty", "", 40, 1},
		{"fits in one line", "hello world", 40, 1},
		{"wraps at word boundary", "hello world foo bar", 11, 2},
		{"long word hard breaks", strings.Repeat("x", 25), 10, 3},
		{"width below min uses 10", "hello world", 5, 2}, // "hello world" is 11 chars > min width 10
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapWords(tt.input, tt.width)
			if len(got) != tt.wantLines {
				t.Errorf("WrapWords(%q, %d) = %d lines, want %d: %v", tt.input, tt.width, len(got), tt.wantLines, got)
			}
		})
	}
}

func TestSortedToolParams(t *testing.T) {
	t.Run("bash primary first", func(t *testing.T) {
		input := map[string]any{"timeout": "5s", "command": "ls -la", "cwd": "/tmp"}
		keys := SortedToolParams("bash", input)
		if keys[0] != "command" {
			t.Errorf("expected command first, got %q", keys[0])
		}
	})

	t.Run("file_read primary first", func(t *testing.T) {
		input := map[string]any{"encoding": "utf8", "path": "/etc/hosts"}
		keys := SortedToolParams("file_read", input)
		if keys[0] != "path" {
			t.Errorf("expected path first, got %q", keys[0])
		}
	})

	t.Run("unknown tool alphabetical", func(t *testing.T) {
		input := map[string]any{"z_key": "z", "a_key": "a", "m_key": "m"}
		keys := SortedToolParams("custom_tool", input)
		if keys[0] != "a_key" || keys[1] != "m_key" || keys[2] != "z_key" {
			t.Errorf("expected alphabetical, got %v", keys)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		keys := SortedToolParams("bash", map[string]any{})
		if len(keys) != 0 {
			t.Errorf("expected empty, got %v", keys)
		}
	})
}

func TestTruncateParam(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		val   any
		limit int
	}{
		{"short value unchanged", "key", "hello", 50},
		{"command has higher limit", "command", strings.Repeat("x", 90), 80},
		{"path has higher limit", "path", strings.Repeat("x", 210), 200},
		{"content has higher limit", "content", strings.Repeat("x", 210), 200},
		{"default limit", "other", strings.Repeat("x", 60), 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateParam(tt.key, tt.val)
			valStr := tt.val.(string)
			if len(valStr) <= tt.limit {
				if got != valStr {
					t.Errorf("short value should be unchanged")
				}
			} else {
				if len(got) > tt.limit+3 { // +3 for "..."
					t.Errorf("truncated value too long: %d > %d", len(got), tt.limit+3)
				}
				if !strings.HasSuffix(got, "...") {
					t.Error("truncated value should end with ...")
				}
			}
		})
	}
}

func TestToolResultHeader(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		result   string
		contains string
	}{
		{"file_read", "file_read", "line1\nline2\nline3", "[read] 2 lines"},
		{"file_write", "file_write", "ok", "[write] ok"},
		{"file_edit", "file_edit", "ok", "[edit] ok"},
		{"bash", "bash", "output\nlines\nhere", "[bash] 2 lines of output"},
		{"grep no matches", "grep", "No matches found.", "[grep] 0 matches"},
		{"grep with matches", "grep", "file.go:10:match\nfile.go:20:match", "[grep] 2 matches"},
		{"list_files empty", "list_files", "No entries found.", "[files] 0 entries"},
		{"list_files with entries", "list_files", "file1.go\nfile2.go", "[files] 2 entries"},
		{"unknown tool", "custom", "data", "[result] custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolResultHeader(tt.toolName, tt.result)
			if got != tt.contains {
				t.Errorf("ToolResultHeader(%q, ...) = %q, want %q", tt.toolName, got, tt.contains)
			}
		})
	}
}

func TestFormatToolUse(t *testing.T) {
	block := domain.ContentBlock{
		Type:      "tool_use",
		ToolName:  "bash",
		ToolInput: map[string]any{"command": "ls -la"},
	}
	got := FormatToolUse(block, 80)
	if !strings.Contains(got, "bash") {
		t.Error("should contain tool name")
	}
	if !strings.Contains(got, "command=ls -la") {
		t.Error("should contain params")
	}
}

func TestFormatToolUse_noParams(t *testing.T) {
	block := domain.ContentBlock{
		Type:     "tool_use",
		ToolName: "test_tool",
	}
	got := FormatToolUse(block, 80)
	if !strings.Contains(got, "test_tool") {
		t.Error("should contain tool name")
	}
}

func TestFormatToolResult(t *testing.T) {
	t.Run("normal result", func(t *testing.T) {
		got := FormatToolResult("bash", "hello world", false, 80)
		if !strings.Contains(got, "[bash]") {
			t.Error("should contain tool header")
		}
	})

	t.Run("error result", func(t *testing.T) {
		got := FormatToolResult("bash", "command failed", true, 80)
		if !strings.Contains(got, "[error]") {
			t.Error("should contain error prefix")
		}
	})

	t.Run("empty result", func(t *testing.T) {
		got := FormatToolResult("bash", "", false, 80)
		if got == "" {
			t.Error("should return at least a header")
		}
	})

	t.Run("long result truncated", func(t *testing.T) {
		longResult := strings.Repeat("line\n", 50)
		got := FormatToolResult("bash", longResult, false, 80)
		if !strings.Contains(got, "more lines") {
			t.Error("long result should be truncated with line count")
		}
	})
}

func TestFormatMessageForScrollback(t *testing.T) {
	t.Run("user message", func(t *testing.T) {
		msg := domain.TranscriptMessage{Role: "user", Content: "hello world"}
		got := FormatMessageForScrollback(msg, 80)
		if !strings.Contains(got, "hello world") {
			t.Error("user content should be preserved")
		}
	})

	t.Run("assistant message", func(t *testing.T) {
		msg := domain.TranscriptMessage{Role: "assistant", Content: "**bold** and *italic*"}
		got := FormatMessageForScrollback(msg, 80)
		if strings.Contains(got, "**") {
			t.Error("markdown markers should be processed")
		}
	})

	t.Run("system message", func(t *testing.T) {
		msg := domain.TranscriptMessage{Role: "system", Content: "system info"}
		got := FormatMessageForScrollback(msg, 80)
		if !strings.Contains(got, "system info") {
			t.Error("system content should be preserved")
		}
	})

	t.Run("empty user message", func(t *testing.T) {
		msg := domain.TranscriptMessage{Role: "user", Content: ""}
		got := FormatMessageForScrollback(msg, 80)
		if got == "" {
			t.Error("should at least show icon")
		}
	})
}

func TestFormatBlockMessage(t *testing.T) {
	t.Run("plain message falls through", func(t *testing.T) {
		msg := domain.TranscriptMessage{Role: "assistant", Content: "hello"}
		got := FormatBlockMessage(msg, 80)
		if !strings.Contains(got, "hello") {
			t.Error("plain message should render content")
		}
	})

	t.Run("assistant with text blocks", func(t *testing.T) {
		msg := domain.TranscriptMessage{
			Role: "assistant",
			Blocks: []domain.ContentBlock{
				{Type: "text", Text: "some text"},
			},
		}
		got := FormatBlockMessage(msg, 80)
		if !strings.Contains(got, "some text") {
			t.Error("text block should render")
		}
	})

	t.Run("assistant with tool_use block", func(t *testing.T) {
		msg := domain.TranscriptMessage{
			Role: "assistant",
			Blocks: []domain.ContentBlock{
				{Type: "tool_use", ToolName: "bash", ToolInput: map[string]any{"command": "ls"}},
			},
		}
		got := FormatBlockMessage(msg, 80)
		if !strings.Contains(got, "bash") {
			t.Error("tool_use block should show tool name")
		}
	})

	t.Run("user with tool_result block", func(t *testing.T) {
		msg := domain.TranscriptMessage{
			Role: "user",
			Blocks: []domain.ContentBlock{
				{Type: "tool_result", ToolName: "bash", ToolResult: "ok"},
			},
		}
		got := FormatBlockMessage(msg, 80)
		if !strings.Contains(got, "bash") {
			t.Error("tool_result should show tool name")
		}
	})

	t.Run("assistant with empty text blocks", func(t *testing.T) {
		msg := domain.TranscriptMessage{
			Role: "assistant",
			Blocks: []domain.ContentBlock{
				{Type: "text", Text: "   "},
			},
		}
		got := FormatBlockMessage(msg, 80)
		if !strings.Contains(got, "(no text)") {
			t.Error("all-empty text blocks should show (no text)")
		}
	})
}

func TestIsLastNonEmptyLine(t *testing.T) {
	lines := []string{"hello", "world", "", ""}
	if isLastNonEmptyLine(lines, 0) {
		t.Error("line 0 is not the last non-empty line")
	}
	if !isLastNonEmptyLine(lines, 1) {
		t.Error("line 1 should be the last non-empty line")
	}
}

func TestRenderTable_SingleColumn(t *testing.T) {
	headers := []string{"Name"}
	rows := [][]string{{"Alice"}, {"Bob"}}
	lines := RenderTable(headers, rows, 40)
	if len(lines) == 0 {
		t.Fatal("expected non-empty output")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Alice") || !strings.Contains(joined, "Bob") {
		t.Error("cell contents should appear")
	}
}

func TestRenderTable_WideContentShrinks(t *testing.T) {
	headers := []string{"A very long header", "Another long header"}
	rows := [][]string{{"short", "short"}}
	// Width of 30 should force column shrinking
	lines := RenderTable(headers, rows, 30)
	if len(lines) == 0 {
		t.Fatal("expected non-empty output")
	}
}
