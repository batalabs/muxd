<h1 align="center">
  <img src="assets/muxd_logo_512.png" alt="muxd" width="220">
  <br>
  muxd
</h1>

<p align="center">
  <b>An open-source AI coding agent that lives in your terminal.</b><br>
  <sub>Multiplex conversations across terminal, hub, and web. Branch, resume, and search your AI history like git.</sub>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/status-early%20release-orange" alt="Early Release">
  <a href="https://github.com/batalabs/muxd/releases"><img src="https://img.shields.io/github/v/release/batalabs/muxd?include_prereleases&label=version" alt="Version"></a>
  <a href="https://github.com/batalabs/muxd/commits/main"><img src="https://img.shields.io/github/last-commit/batalabs/muxd" alt="Last Commit"></a>
  <a href="#install"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <img src="https://img.shields.io/badge/platform-windows%20%7C%20linux%20%7C%20macos-8A2BE2" alt="Windows | Linux | macOS">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="Apache 2.0"></a>
</p>

> 📖 **Full documentation at [muxd.sh/docs](https://muxd.sh/docs)** — setup guides for [client](https://muxd.sh/docs/client) and [hub](https://muxd.sh/docs/hub) modes, [commands](https://muxd.sh/docs/commands), [tools](https://muxd.sh/docs/tools), and [configuration](https://muxd.sh/docs/configuration).

---

## Why muxd?

Most AI coding tools treat each conversation as disposable — close the window and it's gone. muxd saves everything to a local SQLite database so you can close your terminal, reboot, come back next week, and pick up exactly where you left off.

- **Persistent sessions** — conversations survive restarts. Search, branch, and resume any session by project or ID.
- **Branch and fork** — explore alternatives without losing your current thread, like git branches for conversations.
- **Project memory** — the agent remembers your conventions, architecture decisions, and gotchas across sessions.
- **Hub architecture** — run a central hub that coordinates multiple muxd daemons across machines. Connect from any TUI or mobile client and switch between nodes.
- **Multi-channel** — same agent from terminal TUI, headless daemon, hub, or the [mobile app](https://github.com/batalabs/muxd-mobile).
- **25 built-in tools** — file I/O, bash, git, web search, HTTP requests, SMS, scheduling, and more. See the [full list](https://muxd.sh/docs/tools).
- **Any provider** — Claude, GPT, Mistral, Grok, Fireworks, Ollama, and any OpenAI-compatible API. Switch models mid-session.

---

## Demo

<p align="center">
  <img src="assets/muxd_intro.gif" alt="muxd demo" width="700">
</p>

<p align="center">
  <img src="assets/mobile-clients.png" alt="muxd mobile - node picker" height="400">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="assets/mobile-chat.png" alt="muxd mobile - chat" height="400">
</p>

---

## Install

**Windows (PowerShell)**
```powershell
irm https://raw.githubusercontent.com/batalabs/muxd/main/install.ps1 | iex
```

**macOS / Linux**
```bash
curl -fsSL https://raw.githubusercontent.com/batalabs/muxd/main/install.sh | bash
```

**From source** (requires [Go 1.25+](https://go.dev/dl/))
```bash
go install github.com/batalabs/muxd@latest
```

**Prerequisites**: git (for undo/redo) and an API key for at least one [supported provider](https://muxd.sh/docs/configuration).

---

## Quick start

```bash
muxd                              # start a new session
```

Set your API key:
```
/config set anthropic.api_key sk-ant-...
```

Resume a session:
```bash
muxd -c                           # resume latest session
```

See [muxd.sh/docs](https://muxd.sh/docs) for daemon mode, hub setup, slash commands, keybindings, and CLI flags.

---

## Contributing

```bash
git clone https://github.com/batalabs/muxd.git
cd muxd
go build -o muxd.exe .
go test ./...
```

See [muxd.sh/docs/contributing](https://muxd.sh/docs/contributing) for code style and development guide.

---

## License

[Apache License 2.0](LICENSE)
