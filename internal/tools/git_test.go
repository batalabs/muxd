package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitStatusTool(t *testing.T) {
	tool := gitStatusTool()
	if tool.Spec.Name != "git_status" {
		t.Errorf("expected name 'git_status', got %q", tool.Spec.Name)
	}
	if len(tool.Spec.Required) != 0 {
		t.Errorf("expected no required params, got %v", tool.Spec.Required)
	}
	if _, ok := tool.Spec.Properties["path"]; !ok {
		t.Error("expected 'path' property")
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	t.Run("returns git error message outside repo", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Cwd: dir}
		out, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(strings.ToLower(out), "git error:") {
			t.Fatalf("expected git error message, got: %q", out)
		}
	})

	t.Run("returns short status in repo", func(t *testing.T) {
		dir := t.TempDir()
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "test@example.com")
		runGit(t, dir, "config", "user.name", "muxd-test")

		file := filepath.Join(dir, "new.txt")
		if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		ctx := &ToolContext{Cwd: dir}
		out, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "new.txt") {
			t.Fatalf("expected status to include new.txt, got: %q", out)
		}
	})
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
