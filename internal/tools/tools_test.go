package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// file_read
// ---------------------------------------------------------------------------

func TestFileReadTool(t *testing.T) {
	tool := fileReadTool()

	t.Run("reads file with line numbers", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hello.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)

		result, err := tool.Execute(map[string]any{"path": path}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "line1") {
			t.Errorf("expected line1 in result, got: %s", result)
		}
		if !strings.Contains(result, "1 │") {
			t.Errorf("expected line number prefix, got: %s", result)
		}
	})

	t.Run("offset and limit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nums.txt")
		os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path":   path,
			"offset": float64(2),
			"limit":  float64(2),
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "b") {
			t.Errorf("expected line b (offset=2), got: %s", result)
		}
		if strings.Contains(result, "   1 │") {
			t.Errorf("should not contain line 1, got: %s", result)
		}
		// Should have exactly 2 non-empty output lines.
		lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"path": "/nonexistent/file.txt"}, nil)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"path": ""}, nil)
		if err == nil {
			t.Fatal("expected error for empty path")
		}
	})

	t.Run("truncates at 50KB", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.txt")
		// Create a file with enough content to exceed 50KB when line-numbered.
		bigContent := strings.Repeat("x"+strings.Repeat("y", 99)+"\n", 600)
		os.WriteFile(path, []byte(bigContent), 0o644)

		result, err := tool.Execute(map[string]any{"path": path}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "truncated at 50KB") {
			t.Errorf("expected truncation message, got length %d", len(result))
		}
	})
}

// ---------------------------------------------------------------------------
// file_write
// ---------------------------------------------------------------------------

func TestFileWriteTool(t *testing.T) {
	tool := fileWriteTool()

	t.Run("writes new file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out.txt")

		result, err := tool.Execute(map[string]any{
			"path":    path,
			"content": "hello world",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Wrote") {
			t.Errorf("expected Wrote in result, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "hello world" {
			t.Errorf("file content mismatch: %s", data)
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sub", "deep", "file.txt")

		_, err := tool.Execute(map[string]any{
			"path":    path,
			"content": "nested",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "nested" {
			t.Errorf("file content mismatch: %s", data)
		}
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "overwrite.txt")
		os.WriteFile(path, []byte("old"), 0o644)

		_, err := tool.Execute(map[string]any{
			"path":    path,
			"content": "new",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "new" {
			t.Errorf("expected 'new', got: %s", data)
		}
	})

	t.Run("writes empty content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.txt")

		result, err := tool.Execute(map[string]any{
			"path":    path,
			"content": "",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "0 bytes") {
			t.Errorf("expected 0 bytes in result, got: %s", result)
		}
	})
}

// ---------------------------------------------------------------------------
// file_edit
// ---------------------------------------------------------------------------

func TestFileEditTool(t *testing.T) {
	tool := fileEditTool()

	t.Run("single replacement", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "edit.txt")
		os.WriteFile(path, []byte("hello world"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path":       path,
			"old_string": "world",
			"new_string": "earth",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Edited") {
			t.Errorf("expected Edited in result, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "hello earth" {
			t.Errorf("expected 'hello earth', got: %s", data)
		}
	})

	t.Run("zero matches returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "edit.txt")
		os.WriteFile(path, []byte("hello world"), 0o644)

		_, err := tool.Execute(map[string]any{
			"path":       path,
			"old_string": "missing",
			"new_string": "replacement",
		}, nil)
		if err == nil {
			t.Fatal("expected error for zero matches")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %v", err)
		}
	})

	t.Run("multiple matches without replace_all returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "edit.txt")
		os.WriteFile(path, []byte("aaa bbb aaa"), 0o644)

		_, err := tool.Execute(map[string]any{
			"path":       path,
			"old_string": "aaa",
			"new_string": "ccc",
		}, nil)
		if err == nil {
			t.Fatal("expected error for multiple matches")
		}
		if !strings.Contains(err.Error(), "2 times") {
			t.Errorf("expected '2 times' error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "replace_all") {
			t.Errorf("expected error to mention replace_all, got: %v", err)
		}
	})

	t.Run("replace_all replaces all occurrences", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "edit.txt")
		os.WriteFile(path, []byte("foo bar foo baz foo"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path":        path,
			"old_string":  "foo",
			"new_string":  "qux",
			"replace_all": true,
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "3 occurrence(s)") {
			t.Errorf("expected 3 occurrences, got: %s", result)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "qux bar qux baz qux" {
			t.Errorf("expected all foo replaced, got: %s", data)
		}
	})

	t.Run("replace with empty new_string deletes text", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "edit.txt")
		os.WriteFile(path, []byte("remove this"), 0o644)

		_, err := tool.Execute(map[string]any{
			"path":       path,
			"old_string": " this",
			"new_string": "",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "remove" {
			t.Errorf("expected 'remove', got: %s", data)
		}
	})
}

// ---------------------------------------------------------------------------
// bash
// ---------------------------------------------------------------------------

func TestBashTool(t *testing.T) {
	tool := bashTool()

	t.Run("echo command", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{"command": "echo hello"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "hello") {
			t.Errorf("expected 'hello' in output, got: %s", result)
		}
	})

	t.Run("exit code included in result", func(t *testing.T) {
		var cmd string
		if runtime.GOOS == "windows" {
			cmd = "exit /b 42"
		} else {
			cmd = "exit 42"
		}
		result, err := tool.Execute(map[string]any{"command": cmd}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "exit code") {
			t.Errorf("expected 'exit code' in output, got: %s", result)
		}
	})

	t.Run("timeout produces timeout message", func(t *testing.T) {
		var cmd string
		if runtime.GOOS == "windows" {
			cmd = "ping -n 10 127.0.0.1"
		} else {
			cmd = "sleep 10"
		}
		result, err := tool.Execute(map[string]any{
			"command": cmd,
			"timeout": float64(1),
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "timed out") {
			t.Errorf("expected 'timed out' in output, got: %s", result)
		}
	})

	t.Run("max timeout capped at 120", func(t *testing.T) {
		// This just ensures no error -- the cap is tested implicitly.
		result, err := tool.Execute(map[string]any{
			"command": "echo capped",
			"timeout": float64(999),
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "capped") {
			t.Errorf("expected 'capped' in output, got: %s", result)
		}
	})

	t.Run("empty command returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"command": ""}, nil)
		if err == nil {
			t.Fatal("expected error for empty command")
		}
	})
}

// ---------------------------------------------------------------------------
// grep
// ---------------------------------------------------------------------------

func TestGrepTool(t *testing.T) {
	tool := grepTool()

	t.Run("basic match", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\ngoodbye world\n"), 0o644)

		result, err := tool.Execute(map[string]any{
			"pattern": "hello",
			"path":    dir,
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "hello world") {
			t.Errorf("expected match, got: %s", result)
		}
		if !strings.Contains(result, ":1:") {
			t.Errorf("expected line number 1, got: %s", result)
		}
	})

	t.Run("include filter", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.go"), []byte("func main\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "b.txt"), []byte("func main\n"), 0o644)

		result, err := tool.Execute(map[string]any{
			"pattern": "func",
			"path":    dir,
			"include": "*.go",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "a.go") {
			t.Errorf("expected a.go in results, got: %s", result)
		}
		if strings.Contains(result, "b.txt") {
			t.Errorf("should not contain b.txt, got: %s", result)
		}
	})

	t.Run("invalid regex returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"pattern": "[invalid",
		}, nil)
		if err == nil {
			t.Fatal("expected error for invalid regex")
		}
		if !strings.Contains(err.Error(), "invalid regex") {
			t.Errorf("expected 'invalid regex' error, got: %v", err)
		}
	})

	t.Run("no matches returns message", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)

		result, err := tool.Execute(map[string]any{
			"pattern": "zzzzz",
			"path":    dir,
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "No matches found." {
			t.Errorf("expected no matches message, got: %s", result)
		}
	})

	t.Run("skips hidden directories", func(t *testing.T) {
		dir := t.TempDir()
		hiddenDir := filepath.Join(dir, ".hidden")
		os.MkdirAll(hiddenDir, 0o755)
		os.WriteFile(filepath.Join(hiddenDir, "secret.txt"), []byte("findme\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("findme\n"), 0o644)

		result, err := tool.Execute(map[string]any{
			"pattern": "findme",
			"path":    dir,
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(result, ".hidden") {
			t.Errorf("should not search hidden dirs, got: %s", result)
		}
		if !strings.Contains(result, "visible.txt") {
			t.Errorf("should find visible.txt, got: %s", result)
		}
	})

	t.Run("context lines shows surrounding lines", func(t *testing.T) {
		dir := t.TempDir()
		content := "line1\nline2\nMATCH\nline4\nline5\n"
		os.WriteFile(filepath.Join(dir, "ctx.txt"), []byte(content), 0o644)

		result, err := tool.Execute(map[string]any{
			"pattern":       "MATCH",
			"path":          dir,
			"context_lines": float64(1),
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "line2") {
			t.Errorf("expected context line before match, got: %s", result)
		}
		if !strings.Contains(result, "line4") {
			t.Errorf("expected context line after match, got: %s", result)
		}
		if !strings.Contains(result, "MATCH") {
			t.Errorf("expected match line, got: %s", result)
		}
	})
}

// ---------------------------------------------------------------------------
// list_files
// ---------------------------------------------------------------------------

func TestListFilesTool(t *testing.T) {
	tool := listFilesTool()

	t.Run("flat listing", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
		os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0o644)
		os.MkdirAll(filepath.Join(dir, "sub"), 0o755)

		result, err := tool.Execute(map[string]any{"path": dir}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "a.txt") {
			t.Errorf("expected a.txt, got: %s", result)
		}
		if !strings.Contains(result, "b.go") {
			t.Errorf("expected b.go, got: %s", result)
		}
		if !strings.Contains(result, "sub/") {
			t.Errorf("expected sub/ directory marker, got: %s", result)
		}
	})

	t.Run("recursive listing", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
		os.WriteFile(filepath.Join(dir, "top.txt"), []byte("t"), 0o644)
		os.WriteFile(filepath.Join(dir, "sub", "deep.txt"), []byte("d"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path":      dir,
			"recursive": true,
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "sub/deep.txt") {
			t.Errorf("expected sub/deep.txt, got: %s", result)
		}
		if !strings.Contains(result, "top.txt") {
			t.Errorf("expected top.txt, got: %s", result)
		}
	})

	t.Run("include filter", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.go"), []byte("g"), 0o644)
		os.WriteFile(filepath.Join(dir, "b.txt"), []byte("t"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path":    dir,
			"include": "*.go",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "a.go") {
			t.Errorf("expected a.go, got: %s", result)
		}
		if strings.Contains(result, "b.txt") {
			t.Errorf("should not contain b.txt, got: %s", result)
		}
	})

	t.Run("skips hidden directories", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
		os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("c"), 0o644)
		os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("v"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path":      dir,
			"recursive": true,
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(result, ".git") {
			t.Errorf("should not list .git contents, got: %s", result)
		}
		if !strings.Contains(result, "visible.txt") {
			t.Errorf("expected visible.txt, got: %s", result)
		}
	})

	t.Run("entry limit", func(t *testing.T) {
		dir := t.TempDir()
		// Create enough files to potentially trigger truncation message format.
		for i := 0; i < 10; i++ {
			os.WriteFile(filepath.Join(dir, filepath.Base(t.TempDir())+".txt"), []byte("x"), 0o644)
		}

		result, err := tool.Execute(map[string]any{"path": dir}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "No entries found." {
			t.Error("expected some entries")
		}
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{"path": "/nonexistent/dir"}, nil)
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
	})
}

// ---------------------------------------------------------------------------
// FindTool
// ---------------------------------------------------------------------------

func TestFindTool(t *testing.T) {
	t.Run("finds known tool", func(t *testing.T) {
		tool, ok := FindTool("file_read")
		if !ok {
			t.Fatal("expected to find file_read tool")
		}
		if tool.Spec.Name != "file_read" {
			t.Errorf("expected name file_read, got %s", tool.Spec.Name)
		}
	})

	t.Run("returns false for unknown tool", func(t *testing.T) {
		_, ok := FindTool("nonexistent_tool")
		if ok {
			t.Fatal("expected to not find nonexistent tool")
		}
	})
}

// ---------------------------------------------------------------------------
// AllToolSpecs
// ---------------------------------------------------------------------------

func TestAllToolSpecs(t *testing.T) {
	specs := AllToolSpecs()

	t.Run("correct count", func(t *testing.T) {
		expected := 27 // + git_status + x tools + x_search/mentions/reply + x_schedule_list/update/cancel + memory_read/write + schedule_task
		if len(specs) != expected {
			t.Errorf("expected %d tools, got %d", expected, len(specs))
		}
	})

	t.Run("all have names", func(t *testing.T) {
		for _, s := range specs {
			if s.Name == "" {
				t.Error("tool has empty name")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// IsBinary
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ToolNames
// ---------------------------------------------------------------------------

func TestToolNames(t *testing.T) {
	names := ToolNames()
	if len(names) == 0 {
		t.Fatal("expected at least one tool name")
	}
	// Should be sorted.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("names not sorted: %q comes after %q", names[i], names[i-1])
		}
	}
	// Spot-check a few known tools.
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, want := range []string{"file_read", "bash", "grep", "x_post"} {
		if !nameSet[want] {
			t.Errorf("expected %q in tool names", want)
		}
	}
}

// ---------------------------------------------------------------------------
// ToolDisplayName
// ---------------------------------------------------------------------------

func TestToolDisplayName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"web_search", "web_search_brave"},
		{"file_read", "file_read"},
		{"bash", "bash"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolDisplayName(tt.name)
			if got != tt.want {
				t.Errorf("ToolDisplayName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NormalizeToolName
// ---------------------------------------------------------------------------

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"web_search_brave", "web_search"},
		{"WEB_SEARCH_BRAVE", "web_search"},
		{"  Web_Search_Brave  ", "web_search"},
		{"file_read", "file_read"},
		{"BASH", "bash"},
		{"  Grep  ", "grep"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeToolName(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeToolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ToolRiskTags
// ---------------------------------------------------------------------------

func TestToolRiskTags(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"bash", []string{"shell", "write"}},
		{"file_write", []string{"write"}},
		{"file_edit", []string{"write"}},
		{"patch_apply", []string{"write"}},
		{"web_fetch", []string{"network"}},
		{"web_search", []string{"network"}},
		{"x_post", []string{"network", "write"}},
		{"x_search", []string{"network"}},
		{"x_mentions", []string{"network"}},
		{"x_schedule", []string{"network", "write"}},
		{"x_reply", []string{"network", "write"}},
		{"x_schedule_update", []string{"write"}},
		{"x_schedule_cancel", []string{"write"}},
		{"mcp__server__tool", []string{"mcp"}},
		{"file_read", nil},
		{"grep", nil},
		{"list_files", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolRiskTags(tt.name)
			if len(got) != len(tt.want) {
				t.Fatalf("ToolRiskTags(%q) = %v, want %v", tt.name, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ToolRiskTags(%q)[%d] = %q, want %q", tt.name, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ToolProfileDisabledSet
// ---------------------------------------------------------------------------

func TestToolProfileDisabledSet(t *testing.T) {
	t.Run("safe profile disables dangerous tools", func(t *testing.T) {
		disabled := ToolProfileDisabledSet("safe")
		for _, name := range []string{"bash", "web_fetch", "web_search", "x_post", "x_search", "x_mentions", "x_reply", "x_schedule", "x_schedule_update", "x_schedule_cancel"} {
			if !disabled[name] {
				t.Errorf("expected %q disabled in safe profile", name)
			}
		}
		if disabled["file_read"] {
			t.Error("file_read should not be disabled in safe profile")
		}
	})

	t.Run("coder profile disables nothing", func(t *testing.T) {
		disabled := ToolProfileDisabledSet("coder")
		if len(disabled) != 0 {
			t.Errorf("expected empty map for coder, got %v", disabled)
		}
	})

	t.Run("research profile disables write tools", func(t *testing.T) {
		disabled := ToolProfileDisabledSet("research")
		for _, name := range []string{"file_write", "file_edit", "patch_apply", "bash"} {
			if !disabled[name] {
				t.Errorf("expected %q disabled in research profile", name)
			}
		}
		if disabled["grep"] {
			t.Error("grep should not be disabled in research profile")
		}
	})

	t.Run("unknown profile returns empty map", func(t *testing.T) {
		disabled := ToolProfileDisabledSet("nonexistent")
		if len(disabled) != 0 {
			t.Errorf("expected empty map for unknown profile, got %v", disabled)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		disabled := ToolProfileDisabledSet("  SAFE  ")
		if !disabled["bash"] {
			t.Error("expected bash disabled for uppercase SAFE")
		}
	})
}

// ---------------------------------------------------------------------------
// needsPosixShell
// ---------------------------------------------------------------------------

func TestNeedsPosixShell(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"echo hello", false},
		{"cat <<EOF\nhello\nEOF", true},
		{"git commit -m \"$(cat <<'EOF'\nmessage\nEOF\n)\"", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := needsPosixShell(tt.command)
			if got != tt.want {
				t.Errorf("needsPosixShell(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsDeniedConfigFile
// ---------------------------------------------------------------------------

func TestIsDeniedConfigFile(t *testing.T) {
	origGetwd := Getwd
	t.Cleanup(func() { Getwd = origGetwd })

	dir := t.TempDir()
	Getwd = func() (string, error) { return dir, nil }

	t.Run("config.json in cwd is denied", func(t *testing.T) {
		path := filepath.Join(dir, "config.json")
		if !IsDeniedConfigFile(path) {
			t.Error("expected config.json in cwd to be denied")
		}
	})

	t.Run("random file not denied", func(t *testing.T) {
		path := filepath.Join(dir, "readme.md")
		if IsDeniedConfigFile(path) {
			t.Error("expected readme.md to not be denied")
		}
	})

	t.Run("file_read blocks config.json", func(t *testing.T) {
		path := filepath.Join(dir, "config.json")
		os.WriteFile(path, []byte(`{"key":"secret"}`), 0o644)
		tool := fileReadTool()
		_, err := tool.Execute(map[string]any{"path": path}, nil)
		if err == nil {
			t.Fatal("expected error when reading config.json")
		}
		if !strings.Contains(err.Error(), "access denied") {
			t.Errorf("expected 'access denied' error, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// listFilesGlob (via listFilesTool)
// ---------------------------------------------------------------------------

func TestListFilesGlob(t *testing.T) {
	tool := listFilesTool()

	t.Run("glob pattern matches files", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0o644)
		os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0o644)
		os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0o644)

		result, err := tool.Execute(map[string]any{
			"path": filepath.Join(dir, "*.go"),
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "a.go") {
			t.Errorf("expected a.go in results, got: %s", result)
		}
		if !strings.Contains(result, "b.go") {
			t.Errorf("expected b.go in results, got: %s", result)
		}
		if strings.Contains(result, "c.txt") {
			t.Errorf("should not contain c.txt, got: %s", result)
		}
	})

	t.Run("glob with no matches", func(t *testing.T) {
		dir := t.TempDir()
		result, err := tool.Execute(map[string]any{
			"path": filepath.Join(dir, "*.xyz"),
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "No entries found." {
			t.Errorf("expected no entries message, got: %s", result)
		}
	})
}

// ---------------------------------------------------------------------------
// IsBinary
// ---------------------------------------------------------------------------

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "text returns false",
			data: []byte("hello world\nthis is text"),
			want: false,
		},
		{
			name: "null byte returns true",
			data: []byte("hello\x00world"),
			want: true,
		},
		{
			name: "empty returns false",
			data: []byte{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBinary(tt.data)
			if got != tt.want {
				t.Errorf("IsBinary(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}
