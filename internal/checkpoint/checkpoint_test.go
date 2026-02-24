package checkpoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates an isolated git repo in a temp directory with an
// initial commit. It changes cwd to the repo dir and restores it on cleanup.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %s: %v", args, out, err)
		}
	}
	return dir
}

func TestGitRun(t *testing.T) {
	t.Run("returns trimmed stdout", func(t *testing.T) {
		initTestRepo(t)
		out, err := GitRun("rev-parse", "--is-inside-work-tree")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "true" {
			t.Fatalf("expected 'true', got %q", out)
		}
	})

	t.Run("returns error for bad command", func(t *testing.T) {
		initTestRepo(t)
		_, err := GitRun("not-a-real-subcommand")
		if err == nil {
			t.Fatal("expected error for invalid git subcommand")
		}
	})

	t.Run("error includes subcommand name", func(t *testing.T) {
		initTestRepo(t)
		_, err := GitRun("not-a-real-subcommand")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "git not-a-real-subcommand") {
			t.Errorf("error should include subcommand, got: %v", err)
		}
	})
}

func TestDetectGitRepo(t *testing.T) {
	t.Run("inside repo returns true", func(t *testing.T) {
		dir := initTestRepo(t)
		// Resolve symlinks (macOS /tmp -> /private/var/...)
		dir, _ = filepath.EvalSymlinks(dir)
		root, ok := DetectGitRepo()
		if !ok {
			t.Fatal("expected git repo to be detected")
		}
		root, _ = filepath.EvalSymlinks(root)
		// Normalize paths for comparison (Windows may differ in case/separators).
		if filepath.Clean(root) != filepath.Clean(dir) {
			t.Fatalf("expected root %q, got %q", dir, root)
		}
	})

	t.Run("subdirectory returns true", func(t *testing.T) {
		dir := initTestRepo(t)
		sub := filepath.Join(dir, "subdir")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(sub); err != nil {
			t.Fatal(err)
		}
		_, ok := DetectGitRepo()
		if !ok {
			t.Fatal("expected git repo to be detected from subdirectory")
		}
	})

	t.Run("outside repo returns false", func(t *testing.T) {
		dir := t.TempDir() // plain temp dir, no git init
		origDir, _ := os.Getwd()
		t.Cleanup(func() { os.Chdir(origDir) })
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		_, ok := DetectGitRepo()
		if ok {
			t.Fatal("expected git repo NOT to be detected outside a repo")
		}
	})
}

func TestGitStashCreate(t *testing.T) {
	t.Run("dirty tree returns SHA", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Stage the file so the tree is dirty relative to HEAD.
		cmd := exec.Command("git", "add", "file.txt")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %s: %v", out, err)
		}

		sha, err := GitStashCreate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha == "" {
			t.Fatal("expected non-empty SHA for dirty tree")
		}
		if len(sha) < 7 {
			t.Fatalf("SHA looks too short: %q", sha)
		}
	})

	t.Run("clean tree returns empty", func(t *testing.T) {
		initTestRepo(t)
		sha, err := GitStashCreate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "" {
			t.Fatalf("expected empty SHA for clean tree, got %q", sha)
		}
	})

	t.Run("captures untracked files with tracked changes", func(t *testing.T) {
		dir := initTestRepo(t)

		// Create and commit a tracked file first.
		tracked := filepath.Join(dir, "tracked.txt")
		if err := os.WriteFile(tracked, []byte("v1"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "add", "tracked.txt"},
			{"git", "commit", "-m", "add tracked"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s: %v", args, out, err)
			}
		}

		// Modify tracked file and add an untracked file.
		if err := os.WriteFile(tracked, []byte("v2"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}

		sha, err := GitStashCreate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha == "" {
			t.Fatal("expected non-empty SHA when dirty tree + untracked files")
		}
	})
}

func TestGitUpdateRef(t *testing.T) {
	t.Run("invalid SHA returns error", func(t *testing.T) {
		initTestRepo(t)
		err := GitUpdateRef("refs/muxd/test/bad", "not-a-valid-sha")
		if err == nil {
			t.Fatal("expected error for invalid SHA")
		}
	})

	t.Run("creates ref", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "add", "f.txt")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %s: %v", out, err)
		}

		sha, err := GitStashCreate()
		if err != nil || sha == "" {
			t.Fatalf("stash create: sha=%q err=%v", sha, err)
		}

		ref := "refs/muxd/test/1"
		if err := GitUpdateRef(ref, sha); err != nil {
			t.Fatalf("update-ref: %v", err)
		}

		// Verify via show-ref
		out, err := GitRun("show-ref", ref)
		if err != nil {
			t.Fatalf("show-ref: %v", err)
		}
		if !strings.Contains(out, sha) {
			t.Fatalf("expected ref to point at %s, got %q", sha, out)
		}
	})
}

func TestGitDeleteRef(t *testing.T) {
	t.Run("non-existent ref returns nil", func(t *testing.T) {
		initTestRepo(t)
		// Deleting a ref that was never created should not return an error;
		// GitDeleteRef suppresses "not a valid SHA1" errors.
		if err := GitDeleteRef("refs/muxd/does-not-exist"); err != nil {
			t.Fatalf("expected nil error for non-existent ref, got: %v", err)
		}
	})

	t.Run("deletes existing ref", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "add", "f.txt")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %s: %v", out, err)
		}

		sha, _ := GitStashCreate()
		ref := "refs/muxd/test/del"
		_ = GitUpdateRef(ref, sha)

		if err := GitDeleteRef(ref); err != nil {
			t.Fatalf("delete-ref: %v", err)
		}

		// Verify it's gone
		_, err := GitRun("show-ref", ref)
		if err == nil {
			t.Fatal("expected ref to be deleted")
		}
	})
}

func TestGitStashApply(t *testing.T) {
	t.Run("invalid SHA returns error", func(t *testing.T) {
		initTestRepo(t)
		err := GitStashApply("0000000000000000000000000000000000000000")
		if err == nil {
			t.Fatal("expected error for invalid stash SHA")
		}
	})
}

func TestGitRestoreClean(t *testing.T) {
	t.Run("empty commit with untracked files", func(t *testing.T) {
		// When the repo has only an empty initial commit (no tracked files),
		// git checkout -- . fails with "did not match". GitRestoreClean should
		// suppress that and still run git clean -fd.
		dir := initTestRepo(t)
		untracked := filepath.Join(dir, "orphan.txt")
		if err := os.WriteFile(untracked, []byte("stray"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := GitRestoreClean(); err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if _, err := os.Stat(untracked); !os.IsNotExist(err) {
			t.Fatal("expected untracked file to be removed")
		}
	})
}

func TestCreateAndRestoreCheckpoint(t *testing.T) {
	t.Run("round trip: create, modify, restore", func(t *testing.T) {
		dir := initTestRepo(t)

		// Create a file and commit it
		original := "original content"
		fpath := filepath.Join(dir, "test.txt")
		if err := os.WriteFile(fpath, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "add", "test.txt"},
			{"git", "commit", "-m", "add test.txt"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s: %v", args, out, err)
			}
		}

		// Modify the file (dirty working tree)
		modified := "modified content"
		if err := os.WriteFile(fpath, []byte(modified), 0o644); err != nil {
			t.Fatal(err)
		}

		// Create a checkpoint of the dirty state
		sha, err := GitStashCreate()
		if err != nil {
			t.Fatalf("stash create: %v", err)
		}
		if sha == "" {
			t.Fatal("expected non-empty SHA for dirty tree")
		}
		cp := Checkpoint{TurnNumber: 1, SHA: sha}
		ref := "refs/muxd/test-restore/1"
		if err := GitUpdateRef(ref, sha); err != nil {
			t.Fatalf("update-ref: %v", err)
		}

		// Simulate further modification by the agent
		agent := "agent wrote this"
		if err := os.WriteFile(fpath, []byte(agent), 0o644); err != nil {
			t.Fatal(err)
		}

		// Restore the checkpoint (undo the agent's changes)
		if err := GitRestoreClean(); err != nil {
			t.Fatalf("restore clean: %v", err)
		}
		if err := GitStashApply(cp.SHA); err != nil {
			t.Fatalf("stash apply: %v", err)
		}

		// Verify content is back to the modified (pre-agent) state
		data, err := os.ReadFile(fpath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != modified {
			t.Fatalf("expected %q, got %q", modified, string(data))
		}
	})
}

func TestUndoWithUntrackedFiles(t *testing.T) {
	t.Run("checkpoint then agent creates file then undo removes it", func(t *testing.T) {
		dir := initTestRepo(t)

		// Create and commit a tracked file so the repo isn't empty.
		if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "add", "base.txt"},
			{"git", "commit", "-m", "add base"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s: %v", args, out, err)
			}
		}

		// Clean working tree -- checkpoint is clean
		sha, err := GitStashCreate()
		if err != nil {
			t.Fatalf("stash create: %v", err)
		}
		if sha != "" {
			t.Fatal("expected clean tree to produce empty SHA")
		}
		cp := Checkpoint{TurnNumber: 1, IsClean: true}

		// Simulate agent creating a new file
		newFile := filepath.Join(dir, "agent-created.txt")
		if err := os.WriteFile(newFile, []byte("agent output"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Undo: restore clean state (since checkpoint was clean, just reset to HEAD)
		if err := GitRestoreClean(); err != nil {
			t.Fatalf("restore clean: %v", err)
		}
		if !cp.IsClean {
			t.Fatal("expected clean checkpoint")
		}

		// Verify the agent's file is removed
		if _, err := os.Stat(newFile); !os.IsNotExist(err) {
			t.Fatal("expected agent-created file to be removed after undo")
		}

		// Verify the original tracked file is still there
		data, err := os.ReadFile(filepath.Join(dir, "base.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "base" {
			t.Fatalf("expected base.txt to contain 'base', got %q", string(data))
		}
	})
}
