# Agent Tools Reference

muxd's agent has built-in tools across file, shell, search, web, planning, and task categories. This document describes each tool, its parameters, and constraints.

**Categories:**
- [File](#file_read): `file_read`, `file_write`, `file_edit`
- [Shell](#bash): `bash`
- [Search](#grep): `grep`, `list_files`
- [Interaction](#ask_user): `ask_user`
- [Task Management](#todo_read): `todo_read`, `todo_write`
- [Web](#web_search): `web_search`, `web_fetch`
- [Social](#x_post): `x_post`, `x_search`, `x_mentions`, `x_reply`, `x_schedule`, `x_schedule_list`, `x_schedule_update`, `x_schedule_cancel`
- [Git](#git_status): `git_status`
- [Multi-Edit](#patch_apply): `patch_apply`
- [Plan Mode](#plan_enter): `plan_enter`, `plan_exit`
- [Sub-Agent](#task): `task`

## file_read

Read the contents of a file. Returns the file content with line numbers.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `path` | string | yes | Absolute or relative file path to read |
| `offset` | integer | no | Line number to start reading from (1-based, default: 1) |
| `limit` | integer | no | Maximum number of lines to read (default: all) |

### Limits

- Output is truncated at **50 KB**.
- Line numbers are formatted as `%4d | content`.

### Example

```json
{
  "path": "main.go",
  "offset": 10,
  "limit": 30
}
```

---

## file_write

Write content to a file. Creates parent directories if they don't exist. Overwrites the file if it already exists.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `path` | string | yes | File path to write to |
| `content` | string | yes | Content to write to the file |

### Details

- Parent directories are created with permissions `0755`.
- Files are written with permissions `0644`.
- Prefer `file_edit` when modifying existing files to avoid rewriting unchanged content.

### Example

```json
{
  "path": "src/utils.go",
  "content": "package main\n\nfunc hello() string {\n\treturn \"hello\"\n}\n"
}
```

---

## file_edit

Edit a file by replacing an exact string match.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `path` | string | yes | File path to edit |
| `old_string` | string | yes | Exact text to find |
| `new_string` | string | yes | Text to replace it with |
| `replace_all` | boolean | no | Replace all occurrences instead of requiring exactly one (default: false) |

### Details

- The `old_string` must appear exactly once in the file unless `replace_all` is true.
- If there are multiple matches and `replace_all` is false, the tool returns an error with the match count and suggests using `replace_all`.
- Zero matches also returns an error.
- The agent should always read a file before editing to get the exact text.

### Example

```json
{
  "path": "main.go",
  "old_string": "fmt.Println(\"hello\")",
  "new_string": "fmt.Println(\"hello, world\")"
}
```

---

## bash

Run a shell command and return the output.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `command` | string | yes | Shell command to execute |
| `timeout` | integer | no | Timeout in seconds (default: 30, max: 120) |

### Details

- On Windows, uses `cmd /C`. On Unix, uses `sh -c`.
- Captures both stdout and stderr.
- Output is truncated at **50 KB**.
- Timeout is capped at **120 seconds**. On timeout, "(command timed out after Ns)" is appended.
- On non-zero exit, "(exit code: N)" is appended.
- Working directory is set to the session's current directory.

### Example

```json
{
  "command": "go test ./...",
  "timeout": 60
}
```

---

## grep

Search files for a regex pattern. Returns matching lines in `file:line:content` format.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `pattern` | string | yes | Regular expression pattern to search for |
| `path` | string | no | Directory or file to search (default: current directory) |
| `include` | string | no | Glob pattern to filter files (e.g., `*.go`, `*.js`) |
| `context_lines` | integer | no | Number of lines to show before and after each match (like `grep -C`) |

### Limits

- Maximum **200 matches** returned.
- Skips hidden directories and files (names starting with `.`).
- Skips files larger than **1 MB**.
- Skips binary files (detected by null bytes in the first 512 bytes).
- `context_lines` is capped at **10**.
- In context mode, `--` separates non-contiguous groups, `:` prefixes match lines, and a space prefixes context lines.

### Example

```json
{
  "pattern": "func Test",
  "include": "*.go",
  "context_lines": 2
}
```

---

## list_files

List files and directories. Returns entries with a `/` suffix for directories.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `path` | string | no | Directory path to list (default: current directory) |
| `recursive` | boolean | no | List files recursively (default: false) |
| `include` | string | no | Glob pattern to filter file names (e.g., `*.go`) |

### Limits

- Maximum **500 entries** returned.
- Skips hidden directories/files and these directories: `.git`, `.hg`, `.svn`, `.idea`, `.vscode`, `node_modules`, `__pycache__`, `.DS_Store`.
- If `path` contains glob metacharacters (`*`, `?`, `[`), uses glob matching instead of directory listing.
- Recursive mode sorts results alphabetically and uses forward slashes in output.

### Example

```json
{
  "path": "src",
  "recursive": true,
  "include": "*.go"
}
```

---

## ask_user

Ask the user a question and wait for their response. The agent loop pauses until the user replies.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `question` | string | yes | The question to ask the user |

### Details

- The agent loop is **paused** until the user replies. No other tools execute in the meantime.
- Use when you need clarification, a decision, or confirmation before proceeding.
- This tool is always executed **sequentially** (never in parallel with other tools).

### Example

```json
{
  "question": "Should I refactor the database layer to use transactions, or keep the current approach?"
}
```

---

## todo_read

Read the current todo list. Returns all items with their ID, title, status, and optional description.

### Parameters

None. This tool takes no parameters.

### Details

- The todo list is **in-memory and per-session**, it resets when the session ends.
- Returns items in the format: `[id] status - title (description)`.
- Returns `"Todo list is empty."` if no items exist.
- Use this to check progress before planning next steps.

### Example

```json
{}
```

---

## todo_write

Overwrite the todo list with a new set of items. Use this to track multi-step plans.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `todos` | array | yes | Array of todo item objects |

Each todo item object has:

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | yes | Short unique identifier (e.g. `"1"`, `"a"`) |
| `title` | string | yes | Brief task title |
| `status` | string | yes | One of: `pending`, `in_progress`, `completed` |
| `description` | string | no | Longer description of the task |

### Details

- **Overwrites** the entire todo list, always send the full list, not just changes.
- Only **one item** may be `in_progress` at a time. If multiple items have `in_progress` status, the tool returns an error.
- The todo list is in-memory and per-session.
- Returns the updated list as confirmation.

### Example

```json
{
  "todos": [
    {"id": "1", "title": "Read existing code", "status": "completed"},
    {"id": "2", "title": "Implement new feature", "status": "in_progress"},
    {"id": "3", "title": "Write tests", "status": "pending"}
  ]
}
```

---

## web_search

Search the web using the Brave Search API. Returns results with title, URL, and snippet.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `query` | string | yes | Search query |
| `count` | integer | no | Number of results to return (default: 5, max: 20) |

### Details

- Requires the `BRAVE_SEARCH_API_KEY` environment variable to be set.
- Uses the [Brave Search API](https://api.search.brave.com/) free tier.
- Results are formatted as a numbered list with title, URL, and description.
- Returns `"No results found."` if the query yields no results.

### Example

```json
{
  "query": "Go context.WithTimeout best practices",
  "count": 5
}
```

---

## web_fetch

Fetch a URL and return the text content. HTML pages are automatically stripped to plain text.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `url` | string | yes | URL to fetch |

### Limits

- Reads up to **1 MB** of raw content from the response.
- Output is truncated at **50 KB**.
- HTTP timeout is **30 seconds**.

### Details

- HTML content is detected by `Content-Type` header or leading `<` character, and is automatically converted to plain text.
- HTML-to-text conversion strips tags, removes `<script>` and `<style>` blocks, decodes common HTML entities, and collapses whitespace.
- Non-HTML content (JSON, plain text, etc.) is returned as-is.
- Sets a `muxd/1.0` User-Agent header.

### Example

```json
{
  "url": "https://pkg.go.dev/context"
}
```

---

## x_post

Post a tweet via X API v2.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `text` | string | yes | Tweet text (max 280 chars) |

### Details

- Prefers OAuth user access token when configured via `/x auth`.
- Falls back to `X_BEARER_TOKEN` environment variable when user token is absent.
- Returns tweet ID and canonical URL on success.

### Example

```json
{
  "text": "Hello from muxd"
}
```

---

## x_schedule

Schedule a tweet for later posting.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `text` | string | yes | Tweet text (max 280 chars) |
| `time` | string | yes | RFC3339 or `HH:MM` local time |
| `recurrence` | string | no | `once`, `daily`, or `hourly` |

### Details

- Enqueues a scheduled `x_post` tool job.
- Processed by daemon background scheduler.
- Honors scheduler safety policies (`scheduler.allowed_tools`, `tools.disabled`).

### Example

```json
{
  "text": "Scheduled tweet",
  "time": "18:30",
  "recurrence": "daily"
}
```

---

## x_schedule_list

List pending scheduled X posts from the queue.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | integer | no | Maximum number of items to return (default: 20) |

### Details

- Returns a numbered list with ID prefix (8 chars), text preview (first 60 chars), scheduled time, recurrence, and status.
- Returns `"No scheduled X posts."` if the queue is empty.
- Read-only, no risk tags.

### Example

```json
{
  "limit": 10
}
```

---

## x_schedule_update

Update a scheduled X post's text, time, or recurrence.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | yes | Scheduled job ID (or 8-char prefix) |
| `text` | string | no | New tweet text (max 280 chars) |
| `time` | string | no | New schedule time: RFC3339 or `HH:MM` |
| `recurrence` | string | no | New recurrence: `once`, `daily`, or `hourly` |

### Details

- At least one of `text`, `time`, or `recurrence` must be provided.
- Only updates jobs with `status = 'pending'`.
- Tweet text is validated (1-280 characters).
- Disabled in the `safe` tool profile.

### Example

```json
{
  "id": "abcdef12",
  "text": "Updated tweet text",
  "time": "19:00"
}
```

---

## x_schedule_cancel

Cancel a scheduled X post by ID.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | yes | Scheduled job ID (or 8-char prefix) |

### Details

- Marks the job as cancelled. Only cancels jobs with `status = 'pending'` or `'failed'`.
- Disabled in the `safe` tool profile.

### Example

```json
{
  "id": "abcdef12"
}
```

---

## patch_apply

Apply a unified diff patch to one or more files. Use this for making multiple related changes across files in a single operation.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `patch` | string | yes | Unified diff content to apply |

### Details

- Accepts standard unified diff format with `---`/`+++` file headers and `@@` hunk headers.
- Strips `a/` and `b/` prefixes from file paths.
- **Context lines are validated**: if the file content doesn't match the expected context, the patch fails.
- Hunks are applied in reverse order (bottom-up) to preserve line numbers.
- Creates parent directories if needed.
- Returns a summary of applied hunks per file.
- Prefer `file_edit` for simple single-location changes. Use `patch_apply` when making multiple coordinated changes across files.
- **Disabled in plan mode.**

### Example

```json
{
  "patch": "--- a/main.go\n+++ b/main.go\n@@ -10,3 +10,4 @@\n func main() {\n \tfmt.Println(\"hello\")\n+\tfmt.Println(\"world\")\n }\n"
}
```

---

## plan_enter

Enter plan mode. Disables write tools so you can safely explore and analyze before making changes.

### Parameters

None. This tool takes no parameters.

### Details

- In plan mode, the following tools are **disabled**: `file_write`, `file_edit`, `bash`, `patch_apply`.
- Read and search tools remain available: `file_read`, `grep`, `list_files`, `web_search`, `web_fetch`, `todo_read`, `todo_write`, `ask_user`.
- If already in plan mode, returns `"Already in plan mode."`.
- This tool is always executed **sequentially** (never in parallel with other tools).
- Use `plan_exit` to leave plan mode and re-enable write tools.

### Example

```json
{}
```

---

## plan_exit

Exit plan mode and re-enable all write tools.

### Parameters

None. This tool takes no parameters.

### Details

- Re-enables `file_write`, `file_edit`, `bash`, and `patch_apply`.
- If not in plan mode, returns `"Not in plan mode."`.
- This tool is always executed **sequentially** (never in parallel with other tools).

### Example

```json
{}
```

---

## task

Spawn a sub-agent to handle an independent subtask. The sub-agent gets a fresh conversation with the same model and provider.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `description` | string | yes | Short description of the subtask (3-5 words) |
| `prompt` | string | yes | Detailed prompt describing what the sub-agent should do |

### Details

- The sub-agent runs to completion and returns its output.
- Sub-agents have all tools **except `task`** (no recursion / nested sub-agents).
- Sub-agents do not persist messages to the database.
- Output is capped at **50 KB**.
- This tool is always executed **sequentially** (never in parallel with other tools).
- Use for independent subtasks that don't need the main conversation's context.

### Example

```json
{
  "description": "find unused imports",
  "prompt": "Search all Go files in the project for unused imports and list them with file paths and line numbers."
}
```

---

## x_search

Search recent tweets on X/Twitter. Returns matching tweets with author, text, date, URL, and engagement metrics.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `query` | string | yes | Search query (X search syntax) |
| `max_results` | integer | no | Number of results to return (10-100, default 10) |

### Details

- Uses X API v2 recent search endpoint.
- Requires OAuth user access token via `/x auth` or `X_BEARER_TOKEN` env var.
- Returns formatted list with author username, text, date, URL, and engagement counts (likes, retweets, replies).

### Example

```json
{
  "query": "muxd coding agent",
  "max_results": 20
}
```

---

## x_mentions

Fetch recent mentions of the authenticated X/Twitter user.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `max_results` | integer | no | Number of results to return (5-100, default 10) |

### Details

- Requires OAuth user access token (configured via `/x auth`).
- Fetches the authenticated user's ID, then queries their mentions timeline.
- Returns formatted list with author, text, date, URL, and engagement metrics.

### Example

```json
{
  "max_results": 20
}
```

---

## x_reply

Reply to a tweet on X/Twitter.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `tweet_id` | string | yes | The ID of the tweet to reply to |
| `text` | string | yes | Reply text, maximum 280 characters |

### Details

- Posts a reply using X API v2 with `reply.in_reply_to_tweet_id`.
- Requires OAuth user access token via `/x auth` or `X_BEARER_TOKEN` env var.
- Returns the reply tweet ID on success.

### Example

```json
{
  "tweet_id": "1234567890",
  "text": "Thanks for the mention!"
}
```

---

## git_status

Get the git status of the current repository.

### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `path` | string | no | Directory path (default: current working directory) |

### Details

- Runs `git status -s` (short format) in the specified directory.
- Returns stdout/stderr combined, truncated at 50 KB.
- Read-only, safe to run in any mode.

### Example

```json
{
  "path": "."
}
```
