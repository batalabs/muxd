package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/batalabs/muxd/internal/domain"
)

func TestFilterNulls(t *testing.T) {
	tests := []struct {
		name  string
		input []rune
		want  string
	}{
		{"no nulls", []rune("hello"), "hello"},
		{"all nulls", []rune{0, 0, 0}, ""},
		{"mixed", []rune{'a', 0, 'b', 0, 'c'}, "abc"},
		{"empty", []rune{}, ""},
		{"null at start", []rune{0, 'x'}, "x"},
		{"null at end", []rune{'x', 0}, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterNulls(tt.input)
			if got != tt.want {
				t.Errorf("filterNulls(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapsEqualBool(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]bool
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", map[string]bool{}, map[string]bool{}, true},
		{"equal", map[string]bool{"a": true, "b": false}, map[string]bool{"a": true, "b": false}, true},
		{"different length", map[string]bool{"a": true}, map[string]bool{"a": true, "b": false}, false},
		{"different value", map[string]bool{"a": true}, map[string]bool{"a": false}, false},
		{"missing key", map[string]bool{"a": true}, map[string]bool{"b": true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapsEqualBool(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("mapsEqualBool(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDisabledToolsCSV_tui(t *testing.T) {
	tests := []struct {
		name     string
		disabled map[string]bool
		want     string
	}{
		{"nil map", nil, ""},
		{"empty map", map[string]bool{}, ""},
		{"single", map[string]bool{"bash": true}, "bash"},
		{"sorted", map[string]bool{"grep": true, "bash": true}, "bash,grep"},
		{"false excluded", map[string]bool{"bash": true, "grep": false}, "bash"},
		{"all false", map[string]bool{"a": false}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := disabledToolsCSV(tt.disabled)
			if got != tt.want {
				t.Errorf("disabledToolsCSV(%v) = %q, want %q", tt.disabled, got, tt.want)
			}
		})
	}
}

func TestIsBoolConfigKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"footer.tokens", true},
		{"footer.cost", true},
		{"footer.cwd", true},
		{"footer.session", true},
		{"footer.keybindings", true},
		{"model", false},
		{"anthropic.api_key", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isBoolConfigKey(tt.key)
			if got != tt.want {
				t.Errorf("isBoolConfigKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-48 * time.Hour), "2d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TimeAgo(tt.when)
			if got != tt.want {
				t.Errorf("TimeAgo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripTrailingBlankLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no trailing blanks", "hello\nworld", "hello\nworld"},
		{"trailing blank lines", "hello\nworld\n\n\n", "hello\nworld"},
		{"all blank", "\n\n\n", ""},
		{"empty string", "", ""},
		{"single line no trailing", "hello", "hello"},
		{"whitespace-only trailing", "hello\n   \n  ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTrailingBlankLines(tt.in)
			if got != tt.want {
				t.Errorf("stripTrailingBlankLines(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSummarizeForLog(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"short", "hello world", "hello world"},
		{"newlines collapsed", "line1\nline2\nline3", "line1 line2 line3"},
		{"carriage returns collapsed", "a\rb\rc", "a b c"},
		{"extra whitespace collapsed", "a   b   c", "a b c"},
		{"long truncated", strings.Repeat("x", 200), strings.Repeat("x", 180) + "..."},
		{"empty", "", ""},
		{"exactly 180", strings.Repeat("a", 180), strings.Repeat("a", 180)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeForLog(tt.in)
			if got != tt.want {
				t.Errorf("summarizeForLog(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWithInlineCursor(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		cursor int
		want   string
	}{
		{"at start", "hello", 0, "█hello"},
		{"at end", "hello", 5, "hello█"},
		{"in middle", "hello", 2, "he█llo"},
		{"negative clamped", "hello", -5, "█hello"},
		{"past end clamped", "hello", 100, "hello█"},
		{"empty string", "", 0, "█"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withInlineCursor(tt.input, tt.cursor)
			if got != tt.want {
				t.Errorf("withInlineCursor(%q, %d) = %q, want %q", tt.input, tt.cursor, got, tt.want)
			}
		})
	}
}

func TestHardWrapLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		width     int
		wantParts int
	}{
		{"short line unchanged", "hello", 10, 1},
		{"exact fit", "hello", 5, 1},
		{"needs wrapping", "hello world", 5, 3},
		{"empty line", "", 10, 1},
		{"width zero defaults to 1", "abc", 0, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hardWrapLine(tt.line, tt.width)
			if len(got) != tt.wantParts {
				t.Errorf("hardWrapLine(%q, %d) = %d parts, want %d", tt.line, tt.width, len(got), tt.wantParts)
			}
			// Verify reconstruction
			joined := strings.Join(got, "")
			if joined != tt.line {
				t.Errorf("joined = %q, want %q", joined, tt.line)
			}
		})
	}
}

func TestCompactMessages(t *testing.T) {
	t.Run("short conversation unchanged", func(t *testing.T) {
		msgs := make([]domain.TranscriptMessage, 10)
		for i := range msgs {
			if i%2 == 0 {
				msgs[i] = domain.TranscriptMessage{Role: "user", Content: "msg"}
			} else {
				msgs[i] = domain.TranscriptMessage{Role: "assistant", Content: "reply"}
			}
		}
		got := CompactMessages(msgs)
		if len(got) != len(msgs) {
			t.Errorf("short conversation should be unchanged, got %d want %d", len(got), len(msgs))
		}
	})

	t.Run("long conversation compacted", func(t *testing.T) {
		msgs := make([]domain.TranscriptMessage, 50)
		for i := range msgs {
			if i%2 == 0 {
				msgs[i] = domain.TranscriptMessage{Role: "user", Content: "msg"}
			} else {
				msgs[i] = domain.TranscriptMessage{Role: "assistant", Content: "reply"}
			}
		}
		got := CompactMessages(msgs)
		if len(got) >= len(msgs) {
			t.Error("long conversation should be compacted")
		}
		// Should contain compaction notice
		found := false
		for _, m := range got {
			if strings.Contains(m.Content, "compacted") {
				found = true
				break
			}
		}
		if !found {
			t.Error("compacted messages should contain notice")
		}
		// First message should be from original head
		if got[0].Role != "user" || got[0].Content != "msg" {
			t.Error("head should be preserved")
		}
	})

	t.Run("preserves head through first assistant", func(t *testing.T) {
		msgs := make([]domain.TranscriptMessage, 50)
		msgs[0] = domain.TranscriptMessage{Role: "user", Content: "system setup"}
		msgs[1] = domain.TranscriptMessage{Role: "assistant", Content: "acknowledged"}
		for i := 2; i < len(msgs); i++ {
			if i%2 == 0 {
				msgs[i] = domain.TranscriptMessage{Role: "user", Content: "msg"}
			} else {
				msgs[i] = domain.TranscriptMessage{Role: "assistant", Content: "reply"}
			}
		}
		got := CompactMessages(msgs)
		if got[0].Content != "system setup" {
			t.Error("first user message should be preserved in head")
		}
		if got[1].Content != "acknowledged" {
			t.Error("first assistant message should be preserved in head")
		}
	})
}

func TestDefaultRuntimeLogPath(t *testing.T) {
	path := defaultRuntimeLogPath()
	if path == "" {
		t.Skip("config.DataDir returned empty (no home dir)")
	}
	if !strings.HasSuffix(path, "runtime.log") {
		t.Errorf("path = %q, want suffix runtime.log", path)
	}
	if !strings.Contains(path, "logs") {
		t.Errorf("path = %q, want to contain 'logs' directory", path)
	}
}

func TestMustGetwd(t *testing.T) {
	wd := MustGetwd()
	if wd == "" || wd == "." {
		t.Errorf("MustGetwd() = %q, want non-empty absolute path", wd)
	}
}
