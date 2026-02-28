# Plan: Multi-Client Awareness

## Goal

Connected clients (TUI, iOS, Telegram, future web) can see each other — who's online, what session each is in, and basic activity status.

## Phase 1: Client Registry

### Daemon Side (`internal/daemon/`)

- Add `ClientRegistry` to `Server` — tracks connected clients
- Each client gets a record on connect:
  ```
  ClientInfo {
      ID          string    // unique per connection
      Name        string    // user-provided or auto ("MacBook", "iPhone")
      DeviceType  string    // "tui", "ios", "telegram", "web"
      SessionID   string    // which session they're viewing (empty if none)
      ConnectedAt time.Time
      LastSeenAt  time.Time
  }
  ```
- New endpoints:
  - `POST /clients/register` — client announces itself, gets an ID back
  - `GET /clients` — returns list of connected clients
  - `PUT /clients/{id}/session` — client reports which session it's viewing
- Heartbeat: clients ping periodically, daemon prunes stale entries after timeout

### Client Side

- TUI: register on startup with hostname as name
- iOS: register on connect with device name
- Telegram: register per chat ID

## Phase 2: Presence Broadcasting

- New SSE event type: `clientEvent`
  ```json
  {"type": "client_joined",  "client": {...}}
  {"type": "client_left",    "client": {...}}
  {"type": "client_moved",   "clientID": "...", "sessionID": "..."}
  ```
- Daemon broadcasts to all connected SSE streams when client list changes
- Clients render presence info:
  - TUI: status bar showing other clients
  - iOS: badge or indicator in session list ("2 others viewing")
  - Telegram: `/who` command

## Phase 3: Activity Awareness

- Extend `ClientInfo` with activity status: `idle`, `typing`, `streaming`, `tool_running`
- Daemon updates status based on API activity (message submitted, stream started, etc.)
- Clients show real-time status: "iPhone: streaming in refactor-auth"

## Phase 4: Network Access (Multi-Computer)

- Currently daemon binds to `localhost` — single machine only
- To support multiple computers:
  - Bind to `0.0.0.0` with opt-in flag (`--listen 0.0.0.0:7077`)
  - Add token-based auth (API key in config, required in `Authorization` header)
  - TLS support (self-signed cert generation or user-provided)
  - QR code for mobile already exists — extend to include auth token
- Discovery: mDNS/Bonjour for LAN, manual URL for remote

## Out of Scope (For Now)

- Shared editing / collaborative sessions (two clients typing in same session)
- Cross-streaming (watching another client's live output)
- Client-to-client messaging
- Conflict resolution for simultaneous messages to same session

## Files to Create/Modify

| File | Change |
|------|--------|
| `internal/daemon/clients.go` | New — `ClientRegistry`, `ClientInfo` |
| `internal/daemon/server.go` | Register endpoints, wire registry |
| `internal/daemon/sse.go` | Broadcast `clientEvent` messages |
| `internal/tui/model.go` | Register on startup, render presence |
| `internal/tui/statusbar.go` | New — presence display component |
| `MuxdMobile/.../ChatViewModel.swift` | Register on connect |
| `MuxdMobile/.../SessionListView.swift` | Show viewer count per session |
| `internal/telegram/bot.go` | Register per chat, `/who` command |

## Dependencies

- Phase 1-3 work on localhost (single machine, multiple terminals + phone on same network)
- Phase 4 required for true multi-computer support
