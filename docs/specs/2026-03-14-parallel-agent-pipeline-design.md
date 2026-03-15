# Parallel Agent Tasks with Pipeline DAG

**Date:** 2026-03-14
**Status:** Approved

The agent can orchestrate complex work by splitting it into a directed acyclic graph of tasks. Independent tasks run in parallel. Dependent tasks wait for their prerequisites. A DAG visualization shows progress in the terminal. Fail fast on any error.

---

## How It Works

User says: "Refactor the auth module, write tests for the new code, and update the API docs."

The agent recognizes the dependency structure and calls the `parallel` tool:

```json
{
  "tasks": [
    { "id": "1", "description": "refactor auth", "prompt": "Refactor the auth module to use JWT..." },
    { "id": "2", "description": "write auth tests", "prompt": "Write tests for the auth module...", "depends_on": ["1"] },
    { "id": "3", "description": "update API docs", "prompt": "Update the API documentation..." }
  ]
}
```

Tasks 1 and 3 run in parallel (no dependencies between them). Task 2 waits for task 1 to finish, then runs with task 1's output injected as context.

---

## The parallel Tool

```
tool: parallel
description: Run multiple tasks as a pipeline. Tasks with no dependencies run
             simultaneously. Tasks with depends_on wait for prerequisites to
             complete and receive their output as context. Use this when work
             can be broken into independent or staged subtasks.
parameters:
  tasks: (array, required) List of task objects:
    - id: (string) Unique identifier for this task
    - description: (string) Short name for progress display
    - prompt: (string) Full prompt for the sub-agent
    - depends_on: (array of strings, optional) IDs of tasks that must complete first
```

---

## DAG Execution Engine

Core logic lives in `internal/agent/pipeline.go`:

```go
type PipelineTask struct {
    ID          string
    Description string
    Prompt      string
    DependsOn   []string
}

type PipelineResult struct {
    ID          string
    Description string
    Output      string
    Err         error
    Duration    time.Duration
    Status      string // "pending", "running", "done", "failed", "skipped"
}

type Pipeline struct {
    tasks      []PipelineTask
    results    map[string]*PipelineResult
    mu         sync.Mutex
    onProgress func([]PipelineResult)
}

func (p *Pipeline) Run(ctx context.Context, spawn SpawnFunc) error
```

### Run method

1. Validate the DAG: no cycles, all `depends_on` IDs exist, max 8 tasks, max depth 4.
2. Build an adjacency list and compute in-degrees.
3. Start a worker loop:
   - Find all tasks with zero unresolved dependencies.
   - Spawn each as a goroutine via `spawn(description, prompt)`.
   - When a task completes, decrement in-degree of its dependents.
   - Inject predecessor output into dependent task prompts.
   - Repeat until all tasks done or a failure occurs.
4. On any task failure: cancel context, wait for running tasks to finish, mark unstarted dependents as "skipped", return error.

### Cycle detection

Use Kahn's algorithm (topological sort by removing zero in-degree nodes). If any nodes remain after the algorithm completes, the graph has a cycle. Return a clear error listing the cycle participants.

### Dependency injection

When task B depends on task A, task B's prompt is augmented with A's output:

```
[Context from completed task "refactor auth" (14s)]
The auth module has been refactored. Key changes:
- Replaced session tokens with JWT
- New middleware at internal/auth/jwt.go
- Updated 3 handler files

[Your task]
Write comprehensive tests for the auth module...
```

When a task depends on multiple predecessors, all their outputs are injected in order.

---

## DAG Visualization in TUI

New message type `PipelineProgressMsg` sent on every status change.

### Compact mode (2-3 tasks, linear chain)

```
⚡ Pipeline
[✓ refactor auth] ──▶ [● write tests] ──▶ [○ update docs]
```

### Full mode (4+ tasks or branches)

```
⚡ Pipeline (5 tasks)
┌─────────────────┐     ┌──────────────────┐
│ ✓ refactor auth │────▶│ ● write tests    │
└─────────────────┘     └──────────────────┘
┌─────────────────┐            │
│ ✓ update docs   │            ▼
└─────────────────┘     ┌──────────────────┐
                        │ ○ deploy staging │
┌─────────────────┐────▶└──────────────────┘
│ ● lint checks   │
└─────────────────┘

○ pending  ● running  ✓ done  ✗ failed  ⊘ skipped
```

### Rendering

`RenderPipelineDAG(tasks []PipelineResult, deps map[string][]string, width int) string` in `internal/tui/pipeline.go`:

1. Compute layers via topological sort by depth.
2. Render each layer as a row of boxes.
3. Draw arrows between dependent boxes using box-drawing characters.
4. Colors: green for done, yellow for running, dim for pending, red for failed, strikethrough for skipped.
5. Updates in place via `PrintToScrollback` replacing the previous pipeline view.

---

## Implementation

### New files

| File | Responsibility |
|------|---------------|
| `internal/agent/pipeline.go` | `Pipeline` struct, `Run()` DAG execution engine, cycle detection, dependency resolution, topological sort |
| `internal/agent/pipeline_test.go` | Tests: linear chain, diamond DAG, fan-out, cycle detection, fail fast, single task, max depth, max tasks |
| `internal/tools/parallel_tool.go` | `parallel` tool definition, parse tasks, create Pipeline, collect and format results |
| `internal/tui/pipeline.go` | `RenderPipelineDAG()` visualization, `PipelineProgressMsg` type, compact vs full mode |
| `internal/tui/pipeline_test.go` | Tests for DAG rendering (compact, full, various topologies) |

### Modified files

| File | Change |
|------|--------|
| `internal/tools/tools.go` | Add `parallel` to `AllTools()` |
| `internal/tools/task.go` | Add `"parallel"` to `IsSubAgentTool` |
| `internal/tui/model.go` | Handle `PipelineProgressMsg`, replace previous pipeline view on update |
| `internal/agent/submit.go` | Wire `SpawnSubAgent` into `ToolContext` for pipeline use |
| `internal/provider/aliases.go` | Update system prompt: tool count (34), add parallel to tool list and guidelines |

---

## Constraints

- Max 8 tasks per pipeline.
- Max depth 4 (longest dependency chain).
- No cycles. Validated before execution with a clear error message.
- No nested parallelism. `parallel` is excluded from sub-agent tools.
- Each sub-agent has a 60-iteration limit and 50KB output cap.
- Fail fast: any task failure cancels the entire pipeline.
- Sub-agents get the same restricted tool set as the `task` tool.

---

## Edge Cases

- **Single task:** Valid. Executes as a normal sub-agent. No pipeline visualization.
- **All independent:** All tasks run simultaneously. DAG renders as a single row.
- **Linear chain:** A depends on B depends on C. Sequential execution with progress.
- **Diamond:** A and B independent, C depends on both. A and B run in parallel, C waits for both.
- **Fan-out:** A has no dependencies. B, C, D all depend on A. A runs first, then B, C, D run in parallel.
- **Orphan dependency:** Task references a `depends_on` ID that does not exist. Validation error before execution.
- **Empty depends_on:** Treated as no dependencies. Task runs immediately.

---

## Security

- Max task count and depth limits prevent resource exhaustion.
- Sub-agents cannot spawn further sub-agents or parallel pipelines (no recursion).
- Same tool restrictions as existing `task` tool.
- Output per task capped at 50KB.
- Context cancellation propagates to all running sub-agents.

---

## What is NOT in scope

- Git worktree isolation (future upgrade for file conflict prevention).
- `/parallel` slash command (agent-only tool for now).
- Streaming sub-agent deltas to TUI (just start and done events per task).
- Cancelling individual tasks (cancel all or none).
- Retry failed tasks (fail fast; user re-runs the prompt).
- Persistent pipeline state (pipeline is ephemeral; results returned to agent).
- Agent-to-agent communication during execution (sub-agents are independent).
