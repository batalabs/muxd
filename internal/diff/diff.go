package diff

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffSentinel is a marker used to delimit diff sections in streamed content.
const DiffSentinel = "\n\x00DIFF\x00\n"

// maxDiffLines is the maximum number of changed lines (additions + deletions)
// to include in the output before truncating.
const maxDiffLines = 100

// contextLines is the number of unchanged lines shown around each change.
const contextLines = 3

// lineEntry holds a single diff line with its operation type.
type lineEntry struct {
	op   diffmatchpatch.Operation
	text string
}

// ComputeUnifiedDiff produces a unified diff string between old and new text.
// Returns empty string if no changes. Truncates at 100 changed lines.
func ComputeUnifiedDiff(oldText, newText, filename string) string {
	if oldText == newText {
		return ""
	}

	dmp := diffmatchpatch.New()
	oldRunes, newRunes, lineArray := dmp.DiffLinesToRunes(oldText, newText)
	diffs := dmp.DiffMainRunes(oldRunes, newRunes, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	// Expand diffs into per-line entries.
	entries := diffToLineEntries(diffs)
	if len(entries) == 0 {
		return ""
	}

	n := len(entries)

	// Identify which entries are changed.
	changed := make([]bool, n)
	for i, e := range entries {
		changed[i] = e.op != diffmatchpatch.DiffEqual
	}

	// Compute 1-based old/new line numbers for each entry.
	oldLineOf := make([]int, n)
	newLineOf := make([]int, n)
	oldCur, newCur := 1, 1
	for i, e := range entries {
		oldLineOf[i] = oldCur
		newLineOf[i] = newCur
		switch e.op {
		case diffmatchpatch.DiffEqual:
			oldCur++
			newCur++
		case diffmatchpatch.DiffDelete:
			oldCur++
		case diffmatchpatch.DiffInsert:
			newCur++
		}
	}

	// Build hunk groups: contiguous index ranges that need to be shown.
	groups := buildGroups(changed, n)
	if len(groups) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("--- a/" + filename + "\n")
	sb.WriteString("+++ b/" + filename + "\n")

	changedCount := 0
	truncated := false

	for _, g := range groups {
		if truncated {
			break
		}

		oldCount, newCount := countHunkLines(entries, g[0], g[1])
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldLineOf[g[0]], oldCount, newLineOf[g[0]], newCount)

		for k := g[0]; k <= g[1]; k++ {
			e := entries[k]
			switch e.op {
			case diffmatchpatch.DiffEqual:
				sb.WriteString(" " + e.text + "\n")
			case diffmatchpatch.DiffDelete:
				if changedCount >= maxDiffLines {
					truncated = true
				} else {
					sb.WriteString("-" + e.text + "\n")
					changedCount++
				}
			case diffmatchpatch.DiffInsert:
				if changedCount >= maxDiffLines {
					truncated = true
				} else {
					sb.WriteString("+" + e.text + "\n")
					changedCount++
				}
			}
			if truncated {
				break
			}
		}
	}

	if truncated {
		remaining := countTotalChanged(entries) - changedCount
		if remaining < 0 {
			remaining = 0
		}
		fmt.Fprintf(&sb, "... %d more lines (diff truncated)\n", remaining)
	}

	return sb.String()
}

// diffToLineEntries splits a []Diff (each element may span multiple lines) into
// one lineEntry per line.
func diffToLineEntries(diffs []diffmatchpatch.Diff) []lineEntry {
	var entries []lineEntry
	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		// strings.Split of "a\nb\n" gives ["a","b",""] — drop the trailing empty.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, l := range lines {
			entries = append(entries, lineEntry{op: d.Type, text: l})
		}
	}
	return entries
}

// buildGroups returns [start, end] index pairs (inclusive) for hunk windows.
func buildGroups(changed []bool, n int) [][2]int {
	var groups [][2]int
	i := 0
	for i < n {
		if !changed[i] {
			i++
			continue
		}
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i + contextLines
		if end >= n {
			end = n - 1
		}
		// Extend the group to absorb nearby changed lines.
		for {
			extended := false
			for j := end + 1; j < n && j <= end+contextLines+1; j++ {
				if changed[j] {
					newEnd := j + contextLines
					if newEnd >= n {
						newEnd = n - 1
					}
					end = newEnd
					extended = true
				}
			}
			if !extended {
				break
			}
		}
		groups = append(groups, [2]int{start, end})
		i = end + 1
	}
	return groups
}

// countHunkLines returns the number of lines attributed to old and new files
// in entries[start..end] (inclusive).
func countHunkLines(entries []lineEntry, start, end int) (oldCount, newCount int) {
	for k := start; k <= end; k++ {
		switch entries[k].op {
		case diffmatchpatch.DiffEqual:
			oldCount++
			newCount++
		case diffmatchpatch.DiffDelete:
			oldCount++
		case diffmatchpatch.DiffInsert:
			newCount++
		}
	}
	return
}

// countTotalChanged returns the total number of non-equal lines in entries.
func countTotalChanged(entries []lineEntry) int {
	n := 0
	for _, e := range entries {
		if e.op != diffmatchpatch.DiffEqual {
			n++
		}
	}
	return n
}
