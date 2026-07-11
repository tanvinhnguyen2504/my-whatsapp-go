# Workflow — Real-time chat (WebSocket + React frontend)

How inbound WhatsApp messages reach a browser live, how the browser sends messages, and how
history is loaded. For the general server wiring see `workflow.md`; for the WhatsMeow
background see `whatsmeow_workflow.md`.

## What it adds

On top of the REST send API, the server now has a **real-time layer** (`internal/websocket`)
that persists inbound messages to PostgreSQL and broadcasts them to connected browsers over a
WebSocket. A minimal **React + Vite** frontend (`frontend/`) consumes it as a basic chat.

Two facts shape the whole design:

- **The WebSocket is push-only (server → client).** The browser never sends chat content over
  the socket; it reads broadcasts. `Client.Serve` calls `conn.CloseRead`, so app-level frames
  from clients are discarded.
- **Sending is done over REST** (`POST /messages/text`). Receiving is over the socket.

## Components

| Layer | Path | Responsibility |
|-------|------|----------------|
| Models | `internal/websocket/models.go` | `Message` — one struct, `json` (WS frame) + `db` (history row) tags |
| Client | `internal/websocket/client.go` | One subscriber; `Serve` write-pump, buffered `send` chan |
| Hub | `internal/websocket/hub.go` | In-memory registry of clients; `Register`/`Unregister`/`Broadcast` |
| Repo | `internal/websocket/repo.go` | Postgres history: `EnsureSchema`, `Insert`, `List` (table `ws_messages`) |
| Service | `internal/websocket/service.go` | Coordinates hub + repo; `Publish` = persist → broadcast |
| Handler | `internal/websocket/handler.go` | Gin handlers: `Stream`, `History`, `DebugPublish` |
| Routes | `internal/api/routes_websocket.go` | Mounts `/ws`, `/ws/history/:chat`, `/ws/publish` |
| Bridge | `internal/whatsapp` + `cmd/main.go` | Inbound WhatsApp event → `InboundFunc` → `Service.Publish` |
| Frontend | `frontend/` | React + Vite chat UI, dev-proxied to the Go server |

## Endpoints

| Method | Path | Purpose | Payload / result |
|--------|------|---------|------------------|
| `GET` | `/ws` | Open the push stream | Server sends `Message` JSON frames (no envelope) |
| `GET` | `/ws/history/:chat` | Last 50 messages for a chat JID | `Message[]`, newest-first |
| `POST` | `/messages/text` | Send a text message | `{to, body}` → `{message_id, provider}` |
| `POST` | `/ws/publish` | **Dev-only** inject a message (test broadcast) | `Message` body → same `Message` |

## Data model

`Message` (`internal/websocket/models.go`) — the same value is broadcast and persisted:

```go
type Message struct {
    ID        string    `json:"id"         db:"id"`
    ChatJID   string    `json:"chat_jid"   db:"chat_jid"`
    SenderJID string    `json:"sender_jid" db:"sender_jid"`
    Kind      string    `json:"kind"       db:"kind"`       // text | image | video | audio | document | sticker
    Body      string    `json:"body"       db:"body"`       // text content or media caption
    MediaPath string    `json:"media_path,omitempty" db:"media_path"`
    Timestamp time.Time `json:"timestamp"  db:"created_at"`
}
```

Table `ws_messages` is created on startup by `MessageRepo.EnsureSchema`
(`id` PK, `ON CONFLICT DO NOTHING` on insert so re-delivered events are idempotent).

## Flow 1 — Receive (WhatsApp → browser, live)

This is the path wired through the provider's event handler.

```
WhatsApp ──▶ whatsmeow client
                 │  *events.Message
                 ▼
   WhatsMeowProvider.eventHandler         (internal/whatsapp/whatsmeow_provider.go)
                 │  → handleInbound: extract kind/body, save media to disk if any
                 ▼
   onInbound(ctx, whatsapp.InboundMessage) (adapter in cmd/main.go)
                 │  maps → websocket.Message
                 ▼
   websocket.Service.Publish(ctx, msg)
                 ├─ MessageRepo.Insert(msg)   → PostgreSQL (ws_messages)
                 └─ Hub.Broadcast(msg)        → every Client.send channel
                                                    │
                                                    ▼
                                   Client.Serve → conn.Write(JSON)  ──▶ browser onmessage
```

Decoupling note: `internal/whatsapp` does **not** import `internal/websocket`. It exposes a
neutral `InboundMessage` + `InboundFunc` (`provider.go`); `main` supplies the adapter that
targets `Service.Publish`. Because the callback closes over the service, the WebSocket layer
is constructed **before** the provider in `main`.

## Flow 2 — Send (browser → WhatsApp)

The socket can't carry outgoing content, so the UI posts over REST and echoes locally.

```
browser (App.tsx: send)
   │  POST /messages/text {to, body}     (proxied by Vite in dev)
   ▼
api.Handler.sendText → provider.SendText(ctx, to, body)   → WhatsApp
   │  returns {message_id, provider}
   ▼
browser appends an outgoing bubble locally (it is not broadcast back to the sender)
```

Requires the provider to be logged in (`IsReady`). Otherwise `SendText` returns an error,
surfaced as a `502` and shown inline in the composer.

## Flow 3 — History load

```
browser sets "To" → toJID(number)  e.g. 84900000000 → 84900000000@s.whatsapp.net
   │  GET /ws/history/:chat
   ▼
api → websocket.Handler.History → Service.History → MessageRepo.List (LIMIT 50, newest-first)
   ▼
browser reverses to chronological order and renders the thread
```

The UI filters live WS frames to `chat_jid === toJID(to)` so only the open conversation
updates.

## Frontend (`frontend/`)

- `vite.config.ts` — dev proxy: `/ws` (with `ws: true`, also covers `/ws/history/*`) and
  `/messages` → `http://localhost:8082`. The browser is therefore **same-origin** with the
  API, so the WebSocket same-origin check passes and **no CORS** setup is needed.
- `src/useWebSocket.ts` — single `/ws` connection, exposes status, auto-reconnects, and cleans
  up on unmount (safe under React 18 StrictMode double-invoke).
- `src/api.ts` — `sendText`, `getHistory` via `fetch`.
- `src/App.tsx` — connection badge, "To" field, filtered live list + history, send with
  inline errors.

## Run it

```bash
# 1. Backend (needs PostgreSQL; provider need not be logged in for the WS/receive test)
go run ./cmd
# or the hot-reload container: docker compose up   (serves :8082, rebuilds on .go changes)

# 2. Frontend
cd frontend && npm install && npm run dev      # http://localhost:5173
```

## Test the workflow (verified)

```bash
# Receive path without a live WhatsApp inbound — inject a message and watch it broadcast:
curl -X POST localhost:8082/ws/publish -H 'Content-Type: application/json' \
  -d '{"id":"t1","chat_jid":"84900000000@s.whatsapp.net","sender_jid":"84900000000@s.whatsapp.net","kind":"text","body":"hello","timestamp":"2026-07-11T10:00:00Z"}'

# Confirm persistence:
curl 'localhost:8082/ws/history/84900000000@s.whatsapp.net'
```

In the UI, set **To** = `84900000000`; the injected message appears live and in history. Real
inbound WhatsApp messages flow through the same `Publish` path via `onInbound`.

## Known gaps / next steps

- **Media rendering**: inbound media is saved to `received_media/` and referenced by
  `media_path`; the UI shows a `[kind]` label, not the asset. Serving/rendering media is TODO.
- **Broadcast fan-out is unfiltered**: every client receives every message; per-client or
  per-chat subscriptions could be added on the hub.
- **Production serving**: the frontend runs via the Vite dev server. For a single binary,
  `go:embed` the built `dist/` and serve it at `/` (prod Docker copies only the binary).
- **`/ws/publish` is dev-only** — gate or remove it before production.
- Leftover debug prints (`fmt.Println("[DEBUG]…")` / `pkg.DebugJson`) remain in
  `internal/websocket/client.go`.
