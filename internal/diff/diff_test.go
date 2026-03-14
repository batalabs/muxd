package diff

import (
	"strings"
	"testing"
)

func TestComputeUnifiedDiff(t *testing.T) {
	t.Run("no change returns empty string", func(t *testing.T) {
		result := ComputeUnifiedDiff("hello world\n", "hello world\n", "file.txt")
		if result != "" {
			t.Errorf("expected empty string for identical inputs, got %q", result)
		}
	})

	t.Run("single line change", func(t *testing.T) {
		old := "hello world\n"
		new := "hello Go\n"
		result := ComputeUnifiedDiff(old, new, "file.txt")
		if result == "" {
			t.Fatal("expected non-empty diff")
		}
		if !strings.Contains(result, "--- a/file.txt") {
			t.Errorf("missing old file header, got:\n%s", result)
		}
		if !strings.Contains(result, "+++ b/file.txt") {
			t.Errorf("missing new file header, got:\n%s", result)
		}
		if !strings.Contains(result, "-hello world") {
			t.Errorf("missing deletion line, got:\n%s", result)
		}
		if !strings.Contains(result, "+hello Go") {
			t.Errorf("missing addition line, got:\n%s", result)
		}
		if !strings.Contains(result, "@@") {
			t.Errorf("missing hunk header, got:\n%s", result)
		}
	})

	t.Run("empty to content (all additions)", func(t *testing.T) {
		old := ""
		new := "line one\nline two\nline three\n"
		result := ComputeUnifiedDiff(old, new, "new.txt")
		if result == "" {
			t.Fatal("expected non-empty diff")
		}
		if !strings.Contains(result, "+line one") {
			t.Errorf("expected addition of first line, got:\n%s", result)
		}
		if !strings.Contains(result, "+line two") {
			t.Errorf("expected addition of second line, got:\n%s", result)
		}
		if !strings.Contains(result, "+line three") {
			t.Errorf("expected addition of third line, got:\n%s", result)
		}
		if strings.Contains(result, "-") && !strings.Contains(result, "---") {
			t.Errorf("unexpected deletion line in all-additions diff:\n%s", result)
		}
	})

	t.Run("content to empty (all deletions)", func(t *testing.T) {
		old := "alpha\nbeta\ngamma\n"
		new := ""
		result := ComputeUnifiedDiff(old, new, "removed.txt")
		if result == "" {
			t.Fatal("expected non-empty diff")
		}
		if !strings.Contains(result, "-alpha") {
			t.Errorf("expected deletion of alpha, got:\n%s", result)
		}
		if !strings.Contains(result, "-beta") {
			t.Errorf("expected deletion of beta, got:\n%s", result)
		}
		if !strings.Contains(result, "-gamma") {
			t.Errorf("expected deletion of gamma, got:\n%s", result)
		}
	})

	t.Run("multi-hunk change", func(t *testing.T) {
		// Build content where changes are far apart so they form separate hunks.
		var oldLines []string
		var newLines []string
		// 30 shared lines, then a change, 30 more shared lines, then another change.
		for i := 0; i < 30; i++ {
			line := "shared context line\n"
			oldLines = append(oldLines, line)
			newLines = append(newLines, line)
		}
		oldLines = append(oldLines, "old change A\n")
		newLines = append(newLines, "new change A\n")
		for i := 0; i < 30; i++ {
			line := "more shared context\n"
			oldLines = append(oldLines, line)
			newLines = append(newLines, line)
		}
		oldLines = append(oldLines, "old change B\n")
		newLines = append(newLines, "new change B\n")

		old := strings.Join(oldLines, "")
		new := strings.Join(newLines, "")
		result := ComputeUnifiedDiff(old, new, "multi.go")
		if result == "" {
			t.Fatal("expected non-empty diff")
		}
		// Each hunk marker appears twice (opening @@), so count occurrences of "@@ -"
		hunkCount := strings.Count(result, "@@ -")
		if hunkCount < 2 {
			t.Errorf("expected at least 2 hunks, got %d; diff:\n%s", hunkCount, result)
		}
	})

	t.Run("large diff truncation", func(t *testing.T) {
		// Create a diff with 200+ changed lines (all deletions then all additions).
		var oldLines []string
		var newLines []string
		for i := 0; i < 150; i++ {
			oldLines = append(oldLines, "old line\n")
			newLines = append(newLines, "new line\n")
		}
		old := strings.Join(oldLines, "")
		new := strings.Join(newLines, "")
		result := ComputeUnifiedDiff(old, new, "large.go")
		if result == "" {
			t.Fatal("expected non-empty diff for large change")
		}
		if !strings.Contains(result, "truncated") {
			t.Errorf("expected truncation notice for large diff, got:\n%s", result)
		}
		// Count actual changed lines (- or + but not --- or +++).
		lines := strings.Split(result, "\n")
		changedLines := 0
		for _, l := range lines {
			if (strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---")) ||
				(strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++")) {
				changedLines++
			}
		}
		if changedLines > maxDiffLines+5 {
			t.Errorf("expected at most ~%d changed lines after truncation, got %d", maxDiffLines, changedLines)
		}
	})

	t.Run("DiffSentinel constant is defined", func(t *testing.T) {
		if DiffSentinel == "" {
			t.Error("DiffSentinel should not be empty")
		}
		if !strings.Contains(DiffSentinel, "DIFF") {
			t.Errorf("DiffSentinel should contain DIFF, got %q", DiffSentinel)
		}
	})

	t.Run("filename appears in headers", func(t *testing.T) {
		result := ComputeUnifiedDiff("a\n", "b\n", "mypackage/myfile.go")
		if !strings.Contains(result, "mypackage/myfile.go") {
			t.Errorf("expected filename in diff output, got:\n%s", result)
		}
	})
}
