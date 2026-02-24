package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// patch_apply — unified diff parser + applier
// ---------------------------------------------------------------------------

func patchApplyTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "patch_apply",
			Description: "Apply a unified diff patch to one or more files. The patch should be in standard unified diff format (with --- and +++ headers and @@ hunk headers). Context lines are validated. Use this for making multiple related changes across files in a single operation.",
			Properties: map[string]provider.ToolProp{
				"patch": {Type: "string", Description: "Unified diff content to apply"},
			},
			Required: []string{"patch"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			patch, ok := input["patch"].(string)
			if !ok || patch == "" {
				return "", fmt.Errorf("patch is required")
			}

			return applyUnifiedDiff(patch)
		},
	}
}

// applyUnifiedDiff parses and applies a unified diff string.
func applyUnifiedDiff(patch string) (string, error) {
	files, err := parsePatch(patch)
	if err != nil {
		return "", fmt.Errorf("parsing patch: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no file diffs found in patch")
	}

	var results []string
	for _, fd := range files {
		applied, err := applyFileDiff(fd)
		if err != nil {
			return "", fmt.Errorf("applying patch to %s: %w", fd.path, err)
		}
		results = append(results, applied)
	}

	return strings.Join(results, "\n"), nil
}

// fileDiff represents a diff for a single file.
type fileDiff struct {
	path  string
	hunks []hunk
}

// hunk represents a single @@ hunk.
type hunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []diffLine
}

// diffLine is a single line in a hunk.
type diffLine struct {
	op   byte   // ' ' (context), '+' (add), '-' (remove)
	text string // line content without the op prefix
}

// parsePatch parses a unified diff into per-file diffs.
func parsePatch(patch string) ([]fileDiff, error) {
	lines := strings.Split(patch, "\n")
	var files []fileDiff
	var current *fileDiff

	i := 0
	for i < len(lines) {
		line := lines[i]

		// File header: --- a/path or --- path
		if strings.HasPrefix(line, "--- ") {
			// Expect +++ on next line.
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
				i++
				continue
			}
			path := parseFilePath(lines[i+1][4:]) // use +++ path (destination)
			fd := fileDiff{path: path}
			files = append(files, fd)
			current = &files[len(files)-1]
			i += 2
			continue
		}

		// Hunk header: @@ -old,count +new,count @@
		if strings.HasPrefix(line, "@@") && current != nil {
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}

			// Read hunk lines until we've consumed expected lines or hit
			// another header.
			i++
			for i < len(lines) {
				l := lines[i]
				if strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "diff ") {
					break
				}
				if len(l) == 0 {
					// Empty line in patch = context line with empty content.
					h.lines = append(h.lines, diffLine{op: ' ', text: ""})
					i++
					continue
				}
				op := l[0]
				if op != '+' && op != '-' && op != ' ' && op != '\\' {
					// Not a valid diff line — treat as end of hunk.
					break
				}
				if op == '\\' {
					// "\ No newline at end of file" — skip.
					i++
					continue
				}
				h.lines = append(h.lines, diffLine{op: op, text: l[1:]})
				i++
			}
			current.hunks = append(current.hunks, h)
			continue
		}

		// Skip other lines (diff --git, index, etc.)
		i++
	}

	return files, nil
}

// parseFilePath strips common prefix (a/, b/) from diff paths.
func parseFilePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "b/") {
		return raw[2:]
	}
	if strings.HasPrefix(raw, "a/") {
		return raw[2:]
	}
	return raw
}

// parseHunkHeader parses a @@ -old,count +new,count @@ line.
func parseHunkHeader(line string) (hunk, error) {
	// Format: @@ -10,5 +10,7 @@ optional text
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "@@") {
		return hunk{}, fmt.Errorf("not a hunk header: %s", line)
	}

	// Find the closing @@.
	end := strings.Index(line[2:], "@@")
	if end < 0 {
		return hunk{}, fmt.Errorf("malformed hunk header: %s", line)
	}
	inner := strings.TrimSpace(line[2 : 2+end])

	parts := strings.Fields(inner)
	if len(parts) < 2 {
		return hunk{}, fmt.Errorf("malformed hunk header: %s", line)
	}

	oldStart, oldCount, err := parseRange(parts[0])
	if err != nil {
		return hunk{}, fmt.Errorf("old range: %w", err)
	}
	newStart, newCount, err := parseRange(parts[1])
	if err != nil {
		return hunk{}, fmt.Errorf("new range: %w", err)
	}

	return hunk{
		oldStart: oldStart,
		oldCount: oldCount,
		newStart: newStart,
		newCount: newCount,
	}, nil
}

// parseRange parses "-10,5" or "+10,5" or "-10" or "+10".
func parseRange(s string) (int, int, error) {
	if len(s) == 0 {
		return 0, 0, fmt.Errorf("empty range")
	}
	// Strip leading - or +.
	s = s[1:]
	if idx := strings.Index(s, ","); idx >= 0 {
		start, err := strconv.Atoi(s[:idx])
		if err != nil {
			return 0, 0, fmt.Errorf("parsing start: %w", err)
		}
		count, err := strconv.Atoi(s[idx+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("parsing count: %w", err)
		}
		return start, count, nil
	}
	start, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing start: %w", err)
	}
	return start, 1, nil
}

// applyFileDiff applies all hunks to a single file.
func applyFileDiff(fd fileDiff) (string, error) {
	data, err := os.ReadFile(fd.path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("reading %s: %w", fd.path, err)
	}

	var origLines []string
	if len(data) > 0 {
		origLines = strings.Split(string(data), "\n")
	}

	// Apply hunks in reverse order to preserve line numbers.
	// First, sort hunks by oldStart descending.
	hunks := make([]hunk, len(fd.hunks))
	copy(hunks, fd.hunks)
	for i := 0; i < len(hunks); i++ {
		for j := i + 1; j < len(hunks); j++ {
			if hunks[j].oldStart > hunks[i].oldStart {
				hunks[i], hunks[j] = hunks[j], hunks[i]
			}
		}
	}

	result := make([]string, len(origLines))
	copy(result, origLines)

	applied := 0
	for _, h := range hunks {
		newResult, err := applyHunk(result, h)
		if err != nil {
			return "", fmt.Errorf("hunk at line %d: %w", h.oldStart, err)
		}
		result = newResult
		applied++
	}

	// Create parent directories if needed.
	dir := filepath.Dir(fd.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}

	output := strings.Join(result, "\n")
	if err := os.WriteFile(fd.path, []byte(output), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", fd.path, err)
	}

	return fmt.Sprintf("Applied %d hunk(s) to %s", applied, fd.path), nil
}

// applyHunk applies a single hunk to lines.
func applyHunk(lines []string, h hunk) ([]string, error) {
	// oldStart is 1-based. Convert to 0-based.
	startIdx := h.oldStart - 1
	if startIdx < 0 {
		startIdx = 0
	}

	// Validate context lines.
	lineIdx := startIdx
	for _, dl := range h.lines {
		if dl.op == ' ' || dl.op == '-' {
			if lineIdx >= len(lines) {
				return nil, fmt.Errorf("context line %d out of range (file has %d lines)", lineIdx+1, len(lines))
			}
			if lines[lineIdx] != dl.text {
				return nil, fmt.Errorf("context mismatch at line %d: expected %q, got %q", lineIdx+1, dl.text, lines[lineIdx])
			}
			lineIdx++
		}
	}

	// Build new lines: before + hunk result + after.
	var newLines []string
	newLines = append(newLines, lines[:startIdx]...)

	for _, dl := range h.lines {
		switch dl.op {
		case ' ':
			newLines = append(newLines, dl.text)
		case '+':
			newLines = append(newLines, dl.text)
		case '-':
			// skip (removed line)
		}
	}

	afterIdx := startIdx
	for _, dl := range h.lines {
		if dl.op == ' ' || dl.op == '-' {
			afterIdx++
		}
	}
	if afterIdx < len(lines) {
		newLines = append(newLines, lines[afterIdx:]...)
	}

	return newLines, nil
}
