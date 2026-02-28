# Mobile Per-Client Config Dropdown — Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a full-width dropdown overlay to the iOS app's session list toolbar that lets users view connection details and edit model configuration for the connected daemon.

**Architecture:** Custom overlay triggered by the server toolbar button, replacing the current `Menu`. Communicates with daemon via existing `MuxdClient.getConfig()` / `setConfig()` endpoints. iOS 26 liquid glass styling.

---

## Context

- The iOS app (`MuxdMobile`) connects to a muxd daemon over HTTP
- The session list toolbar has a server button that currently shows a `Menu` dropdown with connection details (host, port, token, disconnect)
- `MuxdClient` already has `getConfig()` and `setConfig(key:value:)` wired to `/api/config`
- The daemon hot-reloads config changes (model keys, etc.) immediately
- We just added `model.compact`, `model.title`, `model.tags` config keys to the daemon

## What We're Building

A **full-width custom overlay dropdown** (Slack-style) that slides down from the toolbar button. It replaces the current system `Menu` on the server button in `SessionListView`.

### Visual Reference

Inspired by Slack's channel details dropdown (see `IMG_9706.JPEG`):
- Full width, edge-to-edge
- Dark translucent glass background (iOS 26 liquid glass)
- Grouped sections with icons
- Dismiss by tapping outside or swiping up

### Sections in the Dropdown

#### Section 1: Connection Details (existing info, moved here)
- Server name + host:port
- View Token button
- Copy Address button
- Disconnect button (destructive)

#### Section 2: Model Configuration (new)
- **Model** — text field, reads/writes `model` config key
- **Compact Model** — text field, reads/writes `model.compact` (placeholder: "defaults to main model")
- **Title Model** — text field, reads/writes `model.title` (placeholder: "defaults to main model")
- **Tags Model** — text field, reads/writes `model.tags` (placeholder: "defaults to main model")

Each text field:
- Loads current value from `getConfig()` on dropdown open
- Calls `setConfig(key, value)` on commit (keyboard return / focus loss)
- Shows a subtle checkmark or fade animation on successful save

### Styling — iOS 26 Liquid Glass

- Use `.glassEffect()` modifier for the dropdown container
- Translucent material background with depth/blur
- Rounded corners matching iOS 26 design language
- Section dividers using subtle glass borders
- Icons use SF Symbols with glass-appropriate tinting
- Smooth spring animation for show/hide (slide down from toolbar)

## Entry Point

**File:** `MuxdMobile/MuxdMobile/MuxdMobile/Views/Sessions/SessionListView.swift`

Replace the `Menu { ... } label: { ... }` block (lines ~113-147) with:
1. A `Button` that toggles `@State var showServerPanel: Bool`
2. A `ZStack` overlay that renders `ServerPanelView` when `showServerPanel` is true

## New Files

| File | Purpose |
|------|--------|
| `Views/Sessions/ServerPanelView.swift` | The full-width dropdown overlay with connection details + model config |

## Data Flow

```
User taps server button
  → showServerPanel = true
  → ServerPanelView appears (animated)
  → On appear: client.getConfig() fetches current values
  → User edits a model text field
  → On commit: client.setConfig("model.title", newValue)
  → Daemon hot-reloads, agent picks up new model
  → Subtle save confirmation in UI
  → Tap outside or swipe up → dismiss
```

## API Calls

All existing — no backend changes needed:
- `GET /api/config` — fetch all config as JSON
- `POST /api/config` with `{"key": "model.title", "value": "gpt-4o-mini"}` — set one key

## Scope Boundaries

**In scope:**
- Full-width dropdown overlay with glass effect
- Connection details (moved from current Menu)
- 4 model text fields with live save to daemon
- iOS 26 liquid glass styling

**Out of scope (future):**
- API key editing from mobile
- Footer toggle editing
- Telegram config
- Model picker / suggestions
- Cross-provider per-task models (daemon doesn't support yet)

## Success Criteria

1. Tapping the server button shows a full-width glass dropdown
2. Connection details display correctly (host, port, token, disconnect)
3. All 4 model fields load current values from daemon
4. Editing a field and committing saves to daemon immediately
5. Dropdown dismisses on outside tap or swipe
6. Looks native to iOS 26 with liquid glass effects
