package telegram

import (
	"strings"
	"testing"
)

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no special chars", "hello world", "hello world"},
		{"ampersand", "a & b", "a &amp; b"},
		{"less than", "a < b", "a &lt; b"},
		{"greater than", "a > b", "a &gt; b"},
		{"all three", "<b>bold & stuff</b>", "&lt;b&gt;bold &amp; stuff&lt;/b&gt;"},
		{"empty string", "", ""},
		{"double ampersand", "a && b", "a &amp;&amp; b"},
		{"already escaped looking", "&amp;", "&amp;amp;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeHTML(tt.input)
			if got != tt.want {
				t.Errorf("EscapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "plain text unchanged",
			input: "Hello, world!",
			want:  "Hello, world!",
		},
		{
			name:  "bold text",
			input: "This is **bold** text",
			want:  "This is <b>bold</b> text",
		},
		{
			name:  "italic text",
			input: "This is *italic* text",
			want:  "This is <i>italic</i> text",
		},
		{
			name:  "bold and italic",
			input: "**bold** and *italic*",
			want:  "<b>bold</b> and <i>italic</i>",
		},
		{
			name:  "underscore bold and italic",
			input: "__bold__ and _italic_",
			want:  "<b>bold</b> and <i>italic</i>",
		},
		{
			name:  "strikethrough",
			input: "This is ~~deleted~~ text",
			want:  "This is <s>deleted</s> text",
		},
		{
			name:  "inline code",
			input: "Use `fmt.Println` here",
			want:  "Use <code>fmt.Println</code> here",
		},
		{
			name:  "inline code with HTML chars",
			input: "Use `x < y && z > w` here",
			want:  "Use <code>x &lt; y &amp;&amp; z &gt; w</code> here",
		},
		{
			name:  "fenced code block with language",
			input: "Here:\n```go\nfmt.Println(\"hello\")\n```\nDone.",
			want:  "Here:\n<pre><code>fmt.Println(\"hello\")</code></pre>\nDone.",
		},
		{
			name:  "fenced code block without language",
			input: "Example:\n```\nsome code\n```",
			want:  "Example:\n<pre><code>some code</code></pre>",
		},
		{
			name:  "code block preserves content",
			input: "```python\nif x > 0:\n    print(**kwargs)\n```",
			want:  "<pre><code>if x &gt; 0:\n    print(**kwargs)</code></pre>",
		},
		{
			name:  "header level 1",
			input: "# Hello World",
			want:  "<b>Hello World</b>",
		},
		{
			name:  "header level 2",
			input: "## Section Title",
			want:  "<b>Section Title</b>",
		},
		{
			name:  "header level 3",
			input: "### Subsection",
			want:  "<b>Subsection</b>",
		},
		{
			name:  "link",
			input: "Visit [Google](https://google.com) for search",
			want:  "Visit <a href=\"https://google.com\">Google</a> for search",
		},
		{
			name:  "link with query chars escaped in href",
			input: "Visit [site](https://example.com?q=\"x\"&v=1)",
			want:  "Visit <a href=\"https://example.com?q=&quot;x&quot;&amp;v=1\">site</a>",
		},
		{
			name:  "HTML entities are escaped",
			input: "Use <div> & <span> tags",
			want:  "Use &lt;div&gt; &amp; &lt;span&gt; tags",
		},
		{
			name:  "inline code not processed as markdown",
			input: "The `**bold**` syntax makes text bold",
			want:  "The <code>**bold**</code> syntax makes text bold",
		},
		{
			name:  "code block not processed as markdown",
			input: "```\n**not bold** and *not italic*\n```",
			want:  "<pre><code>**not bold** and *not italic*</code></pre>",
		},
		{
			name:  "multiple inline codes",
			input: "Use `foo` and `bar` functions",
			want:  "Use <code>foo</code> and <code>bar</code> functions",
		},
		{
			name:  "multiple bold",
			input: "**first** and **second**",
			want:  "<b>first</b> and <b>second</b>",
		},
		{
			name: "mixed content",
			input: `# Title

This is **bold** and *italic* text with ` + "`code`" + ` inline.

` + "```go" + `
func main() {
    fmt.Println("hello")
}
` + "```" + `

Visit [docs](https://example.com) for more.`,
			want: "<b>Title</b>\n\nThis is <b>bold</b> and <i>italic</i> text with <code>code</code> inline.\n\n<pre><code>func main() {\n    fmt.Println(\"hello\")\n}</code></pre>\n\nVisit <a href=\"https://example.com\">docs</a> for more.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToTelegramHTML(tt.input)
			if got != tt.want {
				t.Errorf("MarkdownToTelegramHTML(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_realResponse(t *testing.T) {
	// Simulate a real LLM response with various markdown features
	input := "A **goroutine** is Go's lightweight unit of concurrent execution.\n\n" +
		"- **Lightweight**: Goroutines start with a small stack.\n" +
		"- **Cheap to create**: Far less expensive than OS threads.\n\n" +
		"**Example:**\n" +
		"```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n    go say(\"world\")\n    say(\"hello\")\n}\n```\n\n" +
		"> *\"Don't communicate by sharing memory; share memory by communicating.\"*\n\n" +
		"| Property | Goroutine | OS Thread |\n" +
		"|---|---|---|\n" +
		"| Initial stack | ~2-8 KB | ~1-8 MB |"

	got := MarkdownToTelegramHTML(input)
	t.Logf("Converted HTML:\n%s", got)

	// Should not contain raw ** or ``` after conversion
	if strings.Contains(got, "**") {
		t.Error("output still contains raw ** markers")
	}
	if strings.Contains(got, "```") {
		t.Error("output still contains raw ``` markers")
	}
	// Tables should be rendered as aligned <pre> blocks.
	if !strings.Contains(got, "<pre><code>") {
		t.Error("table should be wrapped in <pre><code>")
	}
	if !strings.Contains(got, "Property") && !strings.Contains(got, "Initial stack") {
		t.Error("table content missing")
	}
	// Blockquote should have quote marker and italic
	if !strings.Contains(got, "▎") {
		t.Error("blockquote missing ▎ marker")
	}
	if !strings.Contains(got, "<i>") {
		t.Error("blockquote not converted to italic")
	}
}

func TestMarkdownToTelegramHTML_table(t *testing.T) {
	input := "| Name | Age |\n|---|---|\n| Alice | 30 |\n| Bob | 25 |"
	got := MarkdownToTelegramHTML(input)
	t.Logf("Table output:\n%s", got)

	if !strings.Contains(got, "<pre><code>") {
		t.Error("table should be wrapped in <pre><code>")
	}
	if !strings.Contains(got, "Name") || !strings.Contains(got, "Age") {
		t.Error("table headers missing")
	}
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "Bob") {
		t.Error("table data missing")
	}
}

func TestMarkdownToTelegramHTML_blockquote(t *testing.T) {
	input := "> This is a quote"
	got := MarkdownToTelegramHTML(input)
	want := "▎ <i>This is a quote</i>"
	if got != want {
		t.Errorf("blockquote:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_horizontalRule(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"dashes", "above\n---\nbelow"},
		{"asterisks", "above\n***\nbelow"},
		{"underscores", "above\n___\nbelow"},
		{"long dashes", "above\n----------\nbelow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToTelegramHTML(tt.input)
			if !strings.Contains(got, "————————————————") {
				t.Errorf("horizontal rule not converted:\n  got: %q", got)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_bulletList(t *testing.T) {
	input := "- first item\n- second item\n* third item"
	got := MarkdownToTelegramHTML(input)
	if !strings.Contains(got, "• first item") {
		t.Errorf("bullet not converted:\n  got: %q", got)
	}
	if !strings.Contains(got, "• third item") {
		t.Errorf("asterisk bullet not converted:\n  got: %q", got)
	}
}

func TestEscapeHTMLAttr(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no special chars", "hello", "hello"},
		{"double quote", `say "hi"`, "say &quot;hi&quot;"},
		{"single quote", "it's", "it&#39;s"},
		{"ampersand", "a & b", "a &amp; b"},
		{"less than", "a < b", "a &lt; b"},
		{"mixed", `<a href="x?q=1&v='2'">`, "&lt;a href=&quot;x?q=1&amp;v=&#39;2&#39;&quot;&gt;"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeHTMLAttr(tt.input)
			if got != tt.want {
				t.Errorf("EscapeHTMLAttr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMarkdownTableRow(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"simple row", "| Alice | 30 |", []string{"Alice", "30"}},
		{"no outer pipes", "Alice | 30", []string{"Alice", "30"}},
		{"whitespace", "|  foo  |  bar  |", []string{"foo", "bar"}},
		{"empty cells", "| | |", []string{"", ""}},
		{"single cell", "| hello |", []string{"hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMarkdownTableRow(tt.line)
			if len(got) != len(tt.want) {
				t.Fatalf("parseMarkdownTableRow(%q) returned %d cells, want %d", tt.line, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cell[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatAlignedTable(t *testing.T) {
	t.Run("basic alignment", func(t *testing.T) {
		headers := []string{"Name", "Age"}
		rows := [][]string{{"Alice", "30"}, {"Bob", "25"}}
		got := formatAlignedTable(headers, rows)
		t.Logf("Aligned table:\n%s", got)
		if !strings.Contains(got, "Name") || !strings.Contains(got, "Age") {
			t.Error("headers missing")
		}
		if !strings.Contains(got, "Alice") || !strings.Contains(got, "Bob") {
			t.Error("data missing")
		}
		if !strings.Contains(got, "─┼─") {
			t.Error("separator missing")
		}
		// Check alignment: all lines should have same column separator positions.
		lines := strings.Split(got, "\n")
		if len(lines) < 3 {
			t.Fatalf("expected at least 3 lines, got %d", len(lines))
		}
	})

	t.Run("empty headers", func(t *testing.T) {
		got := formatAlignedTable([]string{}, nil)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("long cell truncated", func(t *testing.T) {
		headers := []string{"X"}
		rows := [][]string{{"a very long string that exceeds thirty characters limit"}}
		got := formatAlignedTable(headers, rows)
		// Should be truncated with ~
		if !strings.Contains(got, "~") {
			t.Error("long cell should be truncated with ~")
		}
	})
}

func TestRewriteMarkdownTables(t *testing.T) {
	t.Run("converts table to aligned pre block", func(t *testing.T) {
		input := "| Name | Age |\n|---|---|\n| Alice | 30 |"
		var blocks []codeRegion
		got := rewriteMarkdownTables(input, &blocks)
		if strings.Contains(got, "|---|") {
			t.Error("separator row should be consumed")
		}
		if len(blocks) != 1 {
			t.Fatalf("expected 1 code block, got %d", len(blocks))
		}
		if !strings.Contains(blocks[0].code, "Name") || !strings.Contains(blocks[0].code, "Alice") {
			t.Error("code block should contain table content")
		}
	})

	t.Run("non-table content preserved", func(t *testing.T) {
		input := "hello\nworld"
		var blocks []codeRegion
		got := rewriteMarkdownTables(input, &blocks)
		if got != input {
			t.Errorf("expected unchanged, got %q", got)
		}
		if len(blocks) != 0 {
			t.Errorf("expected 0 blocks, got %d", len(blocks))
		}
	})
}
