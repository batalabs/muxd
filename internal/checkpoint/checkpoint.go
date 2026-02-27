package checkpoint

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Checkpoint represents a snapshot of the working tree taken before an agent
// turn executes tools. The SHA is a git stash commit object created with
// `git stash create --include-untracked`.
type Checkpoint struct {
	TurnNumber int
	SHA        string // git stash commit SHA (empty if tree was clean)
	IsClean    bool   // true = working tree matched HEAD, no stash needed
}

// ---------------------------------------------------------------------------
// Git helpers (all exported)
// ---------------------------------------------------------------------------

// GitRun executes a git command and returns trimmed stdout.
// Stderr is captured separately so that warnings (e.g. CRLF on Windows)
// don't corrupt the stdout result.
func GitRun(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = out
		}
		return out, fmt.Errorf("git %s: %s: %w", args[0], errMsg, err)
	}
	return out, nil
}

// DetectGitRepo returns the repo root if the cwd is inside a git repo.
func DetectGitRepo() (string, bool) {
	root, err := GitRun("rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	return root, true
}

// GitStashCreate creates a stash commit capturing the full working tree and
// untracked files without touching the stash list or working tree.
// Returns an empty string if the working tree is clean.
func GitStashCreate() (string, error) {
	sha, err := GitRun("stash", "create", "--include-untracked")
	if err != nil {
		return "", err
	}
	return sha, nil
}

// GitUpdateRef creates or updates a git ref to point at the given SHA.
func GitUpdateRef(ref, sha string) error {
	_, err := GitRun("update-ref", ref, sha)
	return err
}

// GitDeleteRef removes a git ref. No error if the ref doesn't exist.
func GitDeleteRef(ref string) error {
	_, err := GitRun("update-ref", "-d", ref)
	// Ignore "not a valid SHA1" / ref-not-found errors
	if err != nil && strings.Contains(err.Error(), "not a valid SHA1") {
		return nil
	}
	return err
}

// GitRestoreClean resets the working tree to HEAD:
// checkout tracked files, then remove untracked files and directories.
// The checkout step is skipped if there are no tracked files (e.g. initial
// empty commit) since git checkout -- . would error in that case.
func GitRestoreClean() error {
	_, err := GitRun("checkout", "--", ".")
	if err != nil && !strings.Contains(err.Error(), "did not match") {
		return err
	}
	_, err = GitRun("clean", "-fd")
	return err
}

// GitStashApply applies a stash commit (by SHA) to the working tree,
// preserving the index state.
func GitStashApply(sha string) error {
	_, err := GitRun("stash", "apply", "--index", sha)
	return err
}
