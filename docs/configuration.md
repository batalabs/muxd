# Configuration `/config`

## API Keys

muxd supports multiple providers. Set API keys via environment variables or `/config`:

```
/config set anthropic.api_key sk-ant-...
/config set openai.api_key sk-...
/config set google.api_key AIza...
/config set brave.api_key BSA...
/config set x.client_id ...
/config set x.client_secret ...
```

Or use environment variables:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
```

Resolution order: environment variable > `/config set` preference.

## `/config` Subcommands

| Command | Description |
|---------|-------------|
| `/config show` | Show all current preferences |
| `/config set <key> <value>` | Set a preference |
| `/config reset` | Reset all preferences to defaults |
| `/config models` | Show available models and pricing |
| `/config tools` | Show available tools |
| `/config messaging` | Show messaging/Telegram config |
| `/config theme` | Show theme settings |

## `/config set` Keys

### Provider Keys

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
| `x.client_id` | X OAuth 2.0 client ID |
| `x.client_secret` | X OAuth 2.0 client secret |
| `x.access_token` | X user access token (managed by `/x auth`) |
| `x.refresh_token` | X refresh token (managed by `/x auth`) |
| `x.token_expiry` | Access token expiry (RFC3339) |
| `x.redirect_url` | Optional OAuth redirect URL override (default localhost callback) |
| `scheduler.allowed_tools` | Comma-separated allowlist of tools permitted in background scheduler |
| `ollama.url` | Ollama server URL (default: `http://localhost:11434`) |

### Model

| Key | Description |
|-----|-------------|
| `model` | Default model (e.g. `claude-sonnet`, `gpt-4o`, `claude-opus`) |

### Footer Display

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `footer.tokens` | bool | `true` | Show token count |
| `footer.cost` | bool | `true` | Show estimated cost |
| `footer.cwd` | bool | `true` | Show current working directory |
| `footer.session` | bool | `true` | Show session info |
| `footer.keybindings` | bool | `true` | Show keybinding hints |

### Telegram

| Key | Description |
|-----|-------------|
| `telegram.bot_token` | Telegram bot token |
| `telegram.allowed_ids` | Comma-separated allowed Telegram user IDs |

Boolean values accept: `true`/`false`, `on`/`off`, `yes`/`no`, `1`/`0` (case-insensitive).

## Model Aliases

Use short aliases instead of full model IDs:

| Alias | Resolves to |
|-------|-------------|
| `claude-sonnet` | `claude-sonnet-4-6` |
| `claude-haiku` | `claude-haiku-4-5-20251001` |
| `claude-opus` | `claude-opus-4-6` |

The `anthropic/` prefix is optional: `anthropic/claude-sonnet` and `claude-sonnet` are equivalent.

```
/config set model claude-opus
```

## Pricing

muxd tracks estimated costs per session.

### Built-in pricing

| Model | Input ($/1M tokens) | Output ($/1M tokens) |
|-------|---------------------|----------------------|
| `claude-opus-4-6` | $5.00 | $25.00 |
| `claude-sonnet-4-6` | $3.00 | $15.00 |
| `claude-haiku-4-5-20251001` | $1.00 | $5.00 |

### Custom pricing

Override or add pricing in `~/.config/muxd/pricing.json`:

```json
{
  "my-custom-model": {
    "input": 1.0,
    "output": 5.0
  }
}
```

Values are dollars per million tokens. Custom entries merge with defaults.

## Data Directory

| Path | Contents |
|------|----------|
| `~/.config/muxd/config.json` | User preferences and API keys |
| `~/.config/muxd/pricing.json` | Custom pricing overrides |
| `~/.local/share/muxd/muxd.db` | SQLite database (sessions and messages) |
