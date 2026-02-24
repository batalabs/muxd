# Undo / Redo

muxd can undo and redo file changes made by the agent, using git under the hood.

## Prerequisites

- You must be working inside a **git repository**. If you're not in a git repo, `/undo` and `/redo` will print an error.
- Git must be installed and available on your `PATH`.

## How It Works

Before each agent turn that involves tool calls, muxd creates a **checkpoint**, a snapshot of your working tree at that point in time.

Under the hood:

1. `git stash create --include-untracked` is run to produce a commit object that captures the full state of the working tree (both tracked changes and untracked files), without actually modifying the stash list or your working tree.
2. The resulting SHA is stored as a hidden git ref at `refs/muxd/<session-prefix>/<turn-number>` so it won't be garbage collected.
3. Checkpoints are kept on an in-memory stack.

## Usage

### Undo

```
/undo
```

Restores your working tree to the state before the last agent turn:

1. Saves the current working tree state so `/redo` can bring it back.
2. Resets the working tree to match HEAD (`git checkout -- .` + `git clean -fd`).
3. If the checkpoint recorded a dirty tree, applies the checkpoint stash to restore those changes.

You can run `/undo` multiple times to walk back through previous turns.

### Redo

```
/redo
```

Re-applies the last undone change. Only available after an `/undo`. The redo stack is **cleared** whenever the agent runs a new turn, so you can only redo changes that haven't been superseded by new agent work.

## Multiple Undo / Redo

Checkpoints and redo states are stacks. Each `/undo` pops from the checkpoint stack and pushes onto the redo stack. Each `/redo` does the reverse. You can undo and redo as many times as there are entries.

```
Turn 1: agent edits file A        checkpoint 1
Turn 2: agent edits file B        checkpoint 2
Turn 3: agent edits file C        checkpoint 3

/undo  → working tree restored to checkpoint 3 (before turn 3)
/undo  → working tree restored to checkpoint 2 (before turn 2)
/redo  → working tree restored to after turn 2
```

## Edge Cases

| Situation | Behavior |
|-----------|----------|
| Not in a git repo | `/undo` and `/redo` print an error |
| No checkpoints | "/undo" prints "Nothing to undo" |
| No redo states | "/redo" prints "Nothing to redo" |
| Agent is running | Cannot undo/redo while the agent loop is active |
| Clean working tree at checkpoint | Checkpoint records `isClean = true` with no stash SHA; restore just resets to HEAD |
| Checkpoint creation fails | A warning is printed but tool execution continues |
| New agent turn after undo | The redo stack is cleared |

## Cleanup

When you start a new session with `/new`, all checkpoint refs for the previous session are deleted from `.git/refs/muxd/`. This prevents accumulation of hidden refs over time.
