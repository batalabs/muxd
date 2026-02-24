# Commands Reference

All commands start with `/` and can be tab-completed.

## /help

Show the list of available commands.

```
/help
```

## /new

Start a new session. Resets all messages, token counts, input history, checkpoints, and the redo stack. If you were in a git repo, old checkpoint refs are cleaned up.

```
/new
```

## /sessions

List up to 10 recent sessions for the current working directory. Each entry shows the session ID prefix (8 characters), title, time since last update, and message count.

```
/sessions
```

Example output:

```
  a1b2c3d4  Fix login bug                2h ago  12 messages
  e5f6g7h8  Refactor auth module         1d ago  34 messages
```

## /continue

Resume a previous session by its ID prefix. Loads all messages back into the conversation and restores token counts.

```
/continue <id-prefix>
```

- **No argument**: behaves the same as `/sessions` (lists recent sessions).
- **With argument**: finds a session whose ID starts with the given prefix and resumes it. If no match is found, prints an error.

## /resume

Alias for `/continue`. Identical behavior.

```
/resume <id-prefix>
```

## /config

Manage preferences and configuration. See also [configuration.md](configuration.md) for full details.

### /config show

Display all preference keys, their current values, and defaults.

```
/config show
```

### /config models

Show available models and pricing.

```
/config models
```

### /config tools

Show available agent tools.

```
/config tools
```

### /config messaging

Show messaging/Telegram configuration.

```
/config messaging
```

### /config theme

Show theme settings.

```
/config theme
```

### /config set

Set a preference value.

```
/config set <key> <value>
```

#### Provider keys

| Key | Description |
|-----|-------------|
| `anthropic.api_key` | Anthropic API key |
| `openai.api_key` | OpenAI API key |
| `google.api_key` | Google AI API key |
| `grok.api_key` | Grok (xAI) API key |
| `mistral.api_key` | Mistral API key |
| `fireworks.api_key` | Fireworks API key |
| `zai.api_key` | zAI API key |
| `brave.api_key` | Brave Search API key (for web search tool) |
| `ollama.url` | Ollama server URL (default: `http://localhost:11434`) |

#### Model

| Key | Description |
|-----|-------------|
| `model` | Default model (e.g. `claude-sonnet`, `gpt-4o`, `claude-opus`) |

#### Footer display

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `footer.tokens` | bool | `true` | Show token count in footer |
| `footer.cost` | bool | `true` | Show estimated cost in footer |
| `footer.cwd` | bool | `true` | Show working directory in footer |
| `footer.session` | bool | `true` | Show session info in footer |
| `footer.keybindings` | bool | `true` | Show keybinding hints in footer |

#### Telegram

| Key | Description |
|-----|-------------|
| `telegram.bot_token` | Telegram bot token |
| `telegram.allowed_ids` | Comma-separated allowed Telegram user IDs |

Boolean values accept: `true`/`false`, `on`/`off`, `yes`/`no`, `1`/`0` (case-insensitive).

Examples:

```
/config set anthropic.api_key sk-ant-...
/config set model claude-opus
/config set footer.cost false
/config set openai.api_key sk-...
```

### /config reset

Reset all preferences to their default values.

```
/config reset
```

## /undo

Undo the last agent turn by restoring the working tree to the checkpoint taken before that turn.

```
/undo
```

- Requires git (must be inside a git repository).
- Cannot undo while the agent is running.
- Prints an error if there are no checkpoints to undo.
- Multiple `/undo` commands walk back through the checkpoint stack.

See [undo-redo.md](undo-redo.md) for a detailed explanation.

## /redo

Re-apply the last undone agent turn.

```
/redo
```

- Only available after an `/undo`.
- The redo stack is cleared whenever the agent runs a new turn.
- Same requirements as `/undo` (git, not during agent loop).

## /clear

Clear the chat display. Resets displayed messages and token counters, but does not end the session. Messages remain persisted in the database.

```
/clear
```

## /exit

Quit muxd.

```
/exit
```

## /quit

Alias for `/exit`.

```
/quit
```

## /tweet

Post, schedule, list, or cancel X posts.

```
/tweet <text>
/tweet --schedule <HH:MM|RFC3339> [--daily|--hourly] <text>
/tweet --list
/tweet --cancel <id>
```

## /schedule

Manage generic scheduled tool jobs.

```
/schedule add <tool> <HH:MM|RFC3339> <json> [--daily|--hourly]
/schedule list
/schedule cancel <id>
```

Example:

```
/schedule add x_post 18:00 {"text":"hello from muxd"}
```

## /x

Manage X OAuth connection.

```
/x auth
/x status
/x logout
```

`/x auth` opens browser-based OAuth and stores access/refresh tokens locally.
