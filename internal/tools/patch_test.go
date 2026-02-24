package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchApplyTool(t *testing.T) {
	tool := patchApplyTool()

	t.Run("applies simple hunk", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hello.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)

		patch := "--- a/hello.txt\n+++ b/hello.txt\n@@ -1,3 +1,3 @@\n line1\n-line2\n+line2_modified\n line3\n"
		// Replace path in patch.
		patch = strings.ReplaceAll(patch, "hello.txt", path)

		result, err := tool.Execute(map[string]any{"patch": patch}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Applied 1 hunk") {
			t.Errorf("expected 'Applied 1 hunk', got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "line2_modified") {
			t.Errorf("expected modified line, got: %s", data)
		}
		if strings.Contains(string(data), "line2\n") {
			t.Errorf("expected old line removed, got: %s", data)
		}
	})

	t.Run("adds new lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "add.txt")
		os.WriteFile(path, []byte("line1\nline2\n"), 0o644)

		patch := "--- a/x\n+++ b/x\n@@ -1,2 +1,4 @@\n line1\n line2\n+line3\n+line4\n"
		patch = strings.ReplaceAll(patch, "x", path)

		result, err := tool.Execute(map[string]any{"patch": patch}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Applied") {
			t.Errorf("expected Applied message, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "line3\nline4") {
			t.Errorf("expected new lines, got: %s", data)
		}
	})

	t.Run("removes lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "del.txt")
		os.WriteFile(path, []byte("keep\nremove_me\nkeep_too\n"), 0o644)

		patch := "--- a/x\n+++ b/x\n@@ -1,3 +1,2 @@\n keep\n-remove_me\n keep_too\n"
		patch = strings.ReplaceAll(patch, "x", path)

		_, err := tool.Execute(map[string]any{"patch": patch}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "remove_me") {
			t.Errorf("expected line removed, got: %s", data)
		}
		if !strings.Contains(string(data), "keep") {
			t.Errorf("expected keep line, got: %s", data)
		}
	})

	t.Run("context mismatch returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mismatch.txt")
		os.WriteFile(path, []byte("actual_content\n"), 0o644)

		patch := "--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n-wrong_content\n+new_content\n"
		patch = strings.ReplaceAll(patch, "x", path)

		_, err := tool.Execute(map[string]any{"patch": patch}, nil)
		if err == nil {
			t.Fatal("expected error for context mismatch")
		}
		if !strings.Contains(err.Error(), "context mismatch") {
			t.Errorf("expected context mismatch error, got: %v", err)
		}
	})

	t.Run("empty patch returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"patch": ""}, nil)
		if err == nil {
			t.Fatal("expected error for empty patch")
		}
	})

	t.Run("patch with no diffs returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"patch": "just some random text\n"}, nil)
		if err == nil {
			t.Fatal("expected error for no diffs")
		}
	})
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOld  int
		wantOldC int
		wantNew  int
		wantNewC int
		wantErr  bool
	}{
		{
			name:     "standard",
			line:     "@@ -10,5 +10,7 @@",
			wantOld:  10,
			wantOldC: 5,
			wantNew:  10,
			wantNewC: 7,
		},
		{
			name:     "with trailing text",
			line:     "@@ -1,3 +1,4 @@ func main() {",
			wantOld:  1,
			wantOldC: 3,
			wantNew:  1,
			wantNewC: 4,
		},
		{
			name:     "single line",
			line:     "@@ -1 +1 @@",
			wantOld:  1,
			wantOldC: 1,
			wantNew:  1,
			wantNewC: 1,
		},
		{
			name:    "malformed",
			line:    "@@ invalid @@",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := parseHunkHeader(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.oldStart != tt.wantOld || h.oldCount != tt.wantOldC {
				t.Errorf("old range = %d,%d, want %d,%d", h.oldStart, h.oldCount, tt.wantOld, tt.wantOldC)
			}
			if h.newStart != tt.wantNew || h.newCount != tt.wantNewC {
				t.Errorf("new range = %d,%d, want %d,%d", h.newStart, h.newCount, tt.wantNew, tt.wantNewC)
			}
		})
	}
}

func TestParseFilePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"b/src/main.go", "src/main.go"},
		{"a/src/main.go", "src/main.go"},
		{"src/main.go", "src/main.go"},
		{" b/foo.txt ", "foo.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFilePath(tt.input)
			if got != tt.want {
				t.Errorf("parseFilePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
