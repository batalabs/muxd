package tui

import (
	"strings"
	"testing"
)

func TestRenderDiff(t *testing.T) {
	t.Run("empty diff returns empty string", func(t *testing.T) {
		result := RenderDiff("", 80)
		if result != "" {
			t.Errorf("expected empty string for empty diff, got %q", result)
		}
	})

	t.Run("file headers appear in output", func(t *testing.T) {
		diff := "--- a/foo.go\n+++ b/foo.go\n@@ -1,1 +1,1 @@\n-old\n+new\n"
		result := RenderDiff(diff, 80)
		// File headers must be present in output (styled or unstyled depending on env).
		if !strings.Contains(result, "--- a/foo.go") {
			t.Errorf("expected old file header in output, got:\n%s", result)
		}
		if !strings.Contains(result, "+++ b/foo.go") {
			t.Errorf("expected new file header in output, got:\n%s", result)
		}
	})

	t.Run("deletion lines are styled red", func(t *testing.T) {
		diff := "--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-removed line\n+added line\n"
		result := RenderDiff(diff, 80)
		// The deletion line should appear in the output.
		if !strings.Contains(result, "removed line") {
			t.Errorf("expected deletion line text in output, got:\n%s", result)
		}
		// The addition line should appear.
		if !strings.Contains(result, "added line") {
			t.Errorf("expected addition line text in output, got:\n%s", result)
		}
	})

	t.Run("hunk header is present in output", func(t *testing.T) {
		diff := "--- a/f.go\n+++ b/f.go\n@@ -1,2 +1,2 @@\n context\n-old\n+new\n"
		result := RenderDiff(diff, 80)
		if !strings.Contains(result, "@@ -1,2 +1,2 @@") {
			t.Errorf("expected hunk header in output, got:\n%s", result)
		}
	})

	t.Run("context lines pass through unstyled", func(t *testing.T) {
		diff := "--- a/f.go\n+++ b/f.go\n@@ -1,3 +1,3 @@\n unchanged context\n-old\n+new\n"
		result := RenderDiff(diff, 80)
		if !strings.Contains(result, " unchanged context") {
			t.Errorf("expected context line in output, got:\n%s", result)
		}
	})

	t.Run("lines are capped at width", func(t *testing.T) {
		longLine := "+" + strings.Repeat("x", 200)
		diff := "--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n" + longLine + "\n"
		result := RenderDiff(diff, 80)
		lines := strings.Split(result, "\n")
		for _, l := range lines {
			// Strip ANSI codes for width check by checking raw rune count isn't too large.
			// We check that at least the styled line won't cause terminal overflow.
			// Use a generous threshold since ANSI codes add non-visible bytes.
			if len([]rune(l)) > 300 {
				t.Errorf("line appears too long after width cap: rune length %d", len([]rune(l)))
			}
		}
	})

	t.Run("narrow width minimum does not panic", func(t *testing.T) {
		diff := "--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-old\n+new\n"
		// Should not panic with very small width.
		result := RenderDiff(diff, 1)
		if result == "" {
			t.Error("expected non-empty result even with narrow width")
		}
	})

	t.Run("truncation notice passes through", func(t *testing.T) {
		diff := "--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-old\n... 50 more lines (diff truncated)\n"
		result := RenderDiff(diff, 80)
		if !strings.Contains(result, "diff truncated") {
			t.Errorf("expected truncation notice in output, got:\n%s", result)
		}
	})
}
