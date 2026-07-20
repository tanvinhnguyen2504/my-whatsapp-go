# Workflow — my-whatsapp

Generated from the codebase. Describes how the server is wired and how a request flows,
end to end. For the conceptual WhatsMeow background see `whatsmeow_workflow.md`; for media
specifics see `sending_media.md`.

## What it is

A Go (Gin) server that represents **one WhatsApp account** and exposes a REST API to send
messages. It supports two interchangeable backends behind one `Provider` interface, selected at
startup by the `WHATSAPP_PROVIDER` env var.

## Startup sequence (`cmd/main.go`)

```
main()
 ├─ slog → JSON logger on stdout
 ├─ config.Load()                 // read .env + env vars, validate
 ├─ whatsapp.New(cfg)             // build the selected Provider
 ├─ provider.Connect(ctx)         // open store, connect / QR-pair
 ├─ scheduler.New(provider)       // in-memory timed sends
 ├─ api.NewRouter(cfg, provider, sched)
 ├─ http.Server on :HTTP_PORT     // goroutine
 └─ wait for SIGINT/SIGTERM → srv.Shutdown (10s) + provider.Disconnect()
```

A failure in `config.Load` or `provider.Connect` logs and exits non-zero.

## Configuration (`internal/config`)

Loaded from `.env` (via `godotenv`) then the environment. `validate()` fails fast per provider.

| Env | Default | Used by |
|-----|---------|---------|
| `MODE` | `development` | general |
| `HTTP_PORT` | `8888` | HTTP server |
| `WHATSAPP_PROVIDER` | `api` | provider selection |
| `DB_HOST` `DB_PORT` `DB_USER` `DB_PASS` `DB_NAME` `DB_SCHEMA` | port `5432` | WhatsMeow Postgres store |
| `WHATSAPP_BUSINESS_PHONE_NUMBER_ID` `WHATSAPP_BUSINESS_ACCESS_TOKEN` `WHATSAPP_BUSINESS_API_VERSION` | version `v21.0` | Graph Cloud API |
| `WHATSAPP_API_WEBHOOK_VERIFY_TOKEN` | — | webhook verification |

`PostgresDSN()` builds `postgres://user[:pass]@host:port/name?sslmode=disable`, appending
`&search_path=<DB_SCHEMA>` when set (the WhatsMeow tables live in that schema).

## Provider selection (`internal/whatsapp/manager.go`)

> Naming note: the values are counter-intuitive but consistent across `manager.go` and
> `config.validate()`.

| `WHATSAPP_PROVIDER` | Builds | Backend | Requires |
|---|---|---|---|
| `business` | `NewWhatsMeowProvider` | **WhatsMeow** — unofficial, personal account, QR login, Postgres session store | `DB_HOST/DB_USER/DB_NAME` |
| `api` | `NewWhatsAppAPIProvider` | **Meta WhatsApp Business Cloud API** — official HTTP Graph API | `WHATSAPP_BUSINESS_PHONE_NUMBER_ID`, `WHATSAPP_BUSINESS_ACCESS_TOKEN` |

Both satisfy `Provider` (`internal/whatsapp/provider.go`):
`Name / Connect / Disconnect / IsReady / QRCode / SendText / SendMedia`.

## WhatsMeow provider (`internal/whatsapp/whatsmeow_provider.go`)

### Connect / session
1. `sql.Open("pgx", dsn)` → `sqlstore.NewWithDB(db, "postgres", …)` → `container.Upgrade(ctx)`
   creates/migrates the `whatsmeow_*` tables in the configured schema.
2. `container.GetFirstDevice(ctx)` loads the saved device (one account).
3. `whatsmeow.NewClient(device, …)` + `AddEventHandler(handleEvent)`.
4. If `client.Store.ID == nil` → **first run** → `pairAndConnect` (QR); else `client.Connect()`
   reuses the stored session (auto-reconnect handled by WhatsMeow).

### QR pairing (`pairAndConnect`)
Opens `GetQRChannel`, connects, and in a goroutine stores the latest `code` into `p.qrCode`
(served by `GET /qr`). On `success` the code is cleared.

### Readiness
`IsReady() = client != nil && IsConnected() && IsLoggedIn()`.

## Sending a message

`SendText` and `SendMedia` both:
1. guard on `IsReady()`,
2. resolve the destination JID via `resolveJID(ctx, to)`,
3. build the proto message,
4. `client.SendMessage(ctx, jid, msg)`.

`resolveJID` currently returns the phone JID `types.NewJID(to, DefaultUserServer)`. It contains a
(currently commented-out) `GetUserInfo` lookup that resolves + persists the recipient's **LID**.
That matters for **never-contacted numbers**: WhatsApp requires a privacy token (`cstoken`)
derived from the recipient LID on a first cold-contact message, and without the LID mapping the
server rejects with **error 463 (`NackCallerReachoutTimelocked`)**. `to` must be E.164 digits,
no `+`.

### Media (`sending_media.md` for detail)
`getWhatMeowMediaType` maps `MediaKind` → `whatsmeow.MediaType` (stickers upload as `MediaImage`).
`client.Upload` returns the encrypted-blob descriptor; `buildMediaMessage` copies its 6 fields
(URL, DirectPath, MediaKey, FileEncSHA256, FileSHA256, FileLength) + `Mimetype` (+ Caption /
FileName) into the matching `waE2E.{Image|Video|Audio|Sticker|Document}Message`.

## Receiving messages (`handleEvent`)

WhatsMeow delivers inbound events in-process (no webhook). The handler logs:
- `*events.Message` → sender/chat/text,
- `*events.Connected`, `*events.LoggedOut` (re-pair required).

(The server does not persist a chat/message history — see `whatsmeow_workflow.md`.)

## HTTP API (`internal/api`)

| Method | Path | Handler | Purpose |
|---|---|---|---|
| GET | `/health` | `health` | status + provider + `ready` |
| GET | `/qr` | `qr` | login QR as PNG (WhatsMeow); JSON when already logged in |
| POST | `/messages/text` | `sendText` | JSON `{to, body}` |
| POST | `/messages/media` | `sendMedia` | multipart `to`, `file`, optional `caption`/`kind` |
| POST | `/messages/schedule` | `scheduleText` | JSON `{to, body, at}` (RFC3339) |
| GET | `/webhook` | `verify` | Meta verification handshake (Cloud API) |
| POST | `/webhook` | `receive` | inbound Cloud API events (logged) |

`sendMedia` reads the file bytes, derives the mimetype (`Content-Type` → `http.DetectContentType`
fallback) and, if `kind` is omitted, classifies it via `kindFromMime`.

## Scheduler (`internal/scheduler`)

`ScheduleText(to, body, at)` uses `time.AfterFunc` to call `provider.SendText` at `at`
(immediately if in the past). In-memory only — jobs do not survive a restart.

## Webhook (`internal/api/webhook.go`)

Only meaningful for the Cloud API (`api`) provider. `GET /webhook` echoes `hub.challenge` when
`hub.verify_token` matches `WHATSAPP_API_WEBHOOK_VERIFY_TOKEN`; `POST /webhook` logs the payload.

## Layout

```
cmd/main.go                     entrypoint, wiring, graceful shutdown
internal/config/                env load + validation + Postgres DSN
internal/whatsapp/
  provider.go                   Provider interface + Media types
  manager.go                    provider factory (WHATSAPP_PROVIDER)
  whatsmeow_provider.go         WhatsMeow (personal account) impl
  whatsapp_api_provider.go      Meta Cloud API (Graph) impl
internal/api/                   Gin router, handlers, webhook
internal/scheduler/             in-memory timed sends
pkg/                            small helpers (DebugJson)
```

## Notes / current considerations

- **Cold-contact 463**: sending to never-messaged numbers can be blocked by WhatsApp
  (`NackCallerReachoutTimelocked`). The `resolveJID` LID pre-resolution (currently commented)
  addresses the token side, but WhatsApp also time-locks unsolicited first contact — prefer
  replying to inbound or messaging known contacts.
- **Single session / single process**: one linked device = one live WebSocket. Do not run two
  processes against the same Postgres session.
- **Media/session store secrecy**: the `whatsmeow_*` tables (esp. `whatsmeow_device`,
  `whatsmeow_sessions`) are login-equivalent secrets; back up, don't share.
