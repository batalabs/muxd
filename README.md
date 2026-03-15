<h1 align="center">
  <img src="assets/muxd_logo_512.png" alt="muxd" width="220">
  <br>
  muxd
</h1>

<p align="center">
  <b>An open source AI coding agent that lives in your terminal.</b><br>
  <sub>33 tools. Any model. Sessions that survive reboots. An agent that builds its own tools.</sub>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/status-early%20release-orange" alt="Early Release">
  <a href="https://github.com/batalabs/muxd/releases"><img src="https://img.shields.io/github/v/release/batalabs/muxd?include_prereleases&label=version" alt="Version"></a>
  <a href="https://github.com/batalabs/muxd/commits/main"><img src="https://img.shields.io/github/last-commit/batalabs/muxd" alt="Last Commit"></a>
  <a href="#install"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <img src="https://img.shields.io/badge/platform-windows%20%7C%20linux%20%7C%20macos-8A2BE2" alt="Windows | Linux | macOS">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="Apache 2.0"></a>
</p>

> **Full docs at [muxd.sh](https://muxd.sh/docs)** · [Client setup](https://muxd.sh/docs/client) · [Hub setup](https://muxd.sh/docs/hub) · [Commands](https://muxd.sh/docs/commands) · [Tools](https://muxd.sh/docs/tools) · [Config](https://muxd.sh/docs/configuration)

---

## What makes muxd different

Most AI coding tools treat conversations as disposable. muxd saves everything to local SQLite. Close your terminal, reboot, come back next week, and pick up exactly where you left off.

### Agent capabilities

| | |
|---|---|
| **33 built in tools** | File I/O, bash, grep, glob, web search, HTTP, SMS, git, scheduling, document reading, and more |
| **Any model** | Claude, GPT, Mistral, Grok, Fireworks, DeepInfra, Ollama, or any OpenAI compatible API |
| **Inline diffs** | Every file edit shows a red and green diff in the chat. See exactly what changed |
| **Read any document** | PDFs, Word, Excel, PowerPoint, HTML, CSV, JSON, XML. No plugins required |
| **Self extending tools** | The agent creates its own tools at runtime. Command templates or scripts, ephemeral or persistent |
| **Second opinion** | Ask a different model for a review. Response shown separately with a crystal ball emoji |

### Session management

| | |
|---|---|
| **Persistent sessions** | Conversations survive restarts. Resume any session by project or ID |
| **Branch and fork** | Explore alternatives without losing your thread. Like git branches for conversations |
| **Project memory** | The agent remembers your conventions and decisions across sessions |
| **Smart compression** | Tiered compaction at 60k/75k/90k tokens. Preserves key decisions while cutting costs |

### Infrastructure

| | |
|---|---|
| **Hub architecture** | Coordinate multiple daemons across machines. Connect from any TUI or mobile client |
| **Always on daemon** | Background service that survives reboots. Auto titles, schedules tasks, runs headless |
| **Mobile app** | [iOS app](https://apps.apple.com/us/app/muxd/id6759869997) connects via QR code. Chat with your agent from anywhere |
| **Hub dispatch** | Send tasks to remote nodes. The agent can delegate work across your machines |

---

## Demo

<p align="center">
  <img src="assets/muxd_intro.gif" alt="muxd demo" width="700">
</p>

<p align="center">
  <img src="assets/mobile-clients.png" alt="muxd mobile - node picker" height="550">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="assets/mobile-chat.png" alt="muxd mobile - chat" height="550">
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

Switch models:
```bash
muxd --model openai/gpt-4o        # use a different model
```

---

## How it works

muxd has three modes.

### Client (default)

Terminal TUI with a built in agent server. Everything runs locally.

```bash
muxd                              # new session
muxd -c                           # resume latest
```

### Daemon

Headless agent server. Connect from other clients without keeping a terminal open.

```bash
muxd --daemon                     # start headless
muxd --daemon --bind 0.0.0.0      # accept remote connections
muxd -service install             # install as system service
```

### Hub

Central coordinator for multiple daemons across machines.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/hub-architecture-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="assets/hub-architecture.svg">
  <img src="assets/hub-architecture.svg" alt="Hub architecture diagram" width="700">
</picture>

```bash
muxd --hub --hub-bind 0.0.0.0                     # start hub
muxd --remote hub-ip:4097 --token <hub-token>      # connect from remote TUI
```

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
