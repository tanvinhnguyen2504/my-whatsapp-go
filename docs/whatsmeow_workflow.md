# Workflow: Building a WhatsApp Server with whatsmeow (personal phone number)

> **Scope:** This document describes the architecture and process of building a Go server that
> represents **one personal WhatsApp number**, using the unofficial `go.mau.fi/whatsmeow`
> library — receiving messages in real time (replacing webhooks) and sending messages via an
> internal HTTP API.
>
> **Does NOT use** the official WhatsApp Cloud API. See `whatsapp-cloud-api-webhook-workflow.md`
> if you need the official approach.

---

## 1. Core concept — differences from the Cloud API

| | Cloud API (official) | whatsmeow (unofficial) |
|---|---|---|
| Nature | Calls Meta's Graph API | Server **emulates a linked device** (like WhatsApp Web) |
| Receiving | Meta POSTs to a public webhook URL | **Real-time push over WebSocket** → in-process event handler |
| Needs public HTTPS? | Required | **No** — the server can sit behind NAT / a private network |
| Phone number | Test number / WABA-registered business number | Personal number, paired via QR |
| Templates / fees | Templates required outside the 24h window, charged | No templates, no fees |
| Risk | None | **ToS violation — the personal number can be banned** on abnormal behavior |
| Scaling | Horizontal (stateless webhook) | **Vertical — 1 session = a single process** |

**Key architectural consequence:** there is no "webhook from Meta". The "webhook" in this system
is an **in-process event handler + dispatcher** that you build yourself.

---

## 2. Overall architecture

```
[Personal phone]             [WhatsApp servers]           [Go server]
  (WhatsApp app)                                        (linked device)
        |                            |                          |
        |---- pair QR (once) ------>|<===== WebSocket ========>|
        |                            |      (E2E encrypted)     |
                                     |                          |
[Recipient] ------ messages ------->|--- events.Message ------>| EventHandler
                                     |                          |     |
        <--------------------------- |<--- SendMessage ---------| Dispatcher
                                     |                          |     |
                                     |                          v     v
                                     |                   [HTTP API]  [Bot logic /
                                     |                   POST /send   internal webhook]
                                     |                          ^
                                     |                   [Media Resolver (GCS)]
```

**Components:**

| Component | Role |
|---|---|
| **whatsmeow Client** | Holds the WebSocket, Signal encryption, sends/receives messages |
| **SQL Store** | Stores the session + encryption keys (SQLite / Postgres) — lose it and you must re-pair |
| **EventHandler** | Receives every event, filters, pushes to an internal queue |
| **Dispatcher** | Turns events → normalized payload → forwards to bot logic |
| **HTTP Send API** | Internal endpoint for other systems to send messages via your number |
| **Media Resolver** | Fetches bytes from GCS, uploads to WhatsApp, caches by generation |

---

## 3. Phase 1 — Pairing (done once)

After `client.Connect()`, the client authenticates automatically if the store already has a
session; if not, it emits a QR event to establish a new link.

**Option A — QR code (recommended for the first time):**

```go
if client.Store.ID == nil { // no session yet
    qrChan, _ := client.GetQRChannel(context.Background())
    err = client.Connect()
    // ...
    for evt := range qrChan {
        if evt.Event == "code" {
            qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
        } else {
            fmt.Println("Pairing event:", evt.Event) // "success" | "timeout" | "error"
        }
    }
} else {
    err = client.Connect() // session exists, connect directly
}
```

On the phone: **Settings → Linked Devices → Link a Device** → scan the QR.

**Option B — Pairing code (headless server):** use `client.PairPhone(phone, true, ...)` to
generate a code and enter it on the phone instead of scanning a QR. Note you must `Connect()`
before calling it (the QR event is still emitted but can be ignored).

**After pairing:**
- The phone does NOT need to stay online continuously (multi-device protocol).
- Phone offline > ~14 days → the linked device is disconnected → you must re-pair.
- The server takes 1 slot out of the account's maximum of 4 linked devices.

---

## 4. Phase 2 — Session persistence

Whatsmeow **requires** a storage backend; without one you have to scan a new QR on every
restart. The SQL store supports SQLite and PostgreSQL.

```go
// SQLite (single instance, simple):
container, err := sqlstore.New(ctx, "sqlite3",
    "file:/data/wa-session.db?_foreign_keys=on", dbLog)
// remember to import _ "github.com/mattn/go-sqlite3"

deviceStore, err := container.GetFirstDevice(ctx) // only one number → GetFirstDevice
client := whatsmeow.NewClient(deviceStore, clientLog)
```

**Choosing by infrastructure:**

| Deploy | Store | Notes |
|---|---|---|
| GCE VM / GKE + PVC | SQLite on a persistent disk | Simplest |
| Stateless-ish container | Cloud SQL Postgres (`pgx` driver) | Restart/reschedule without losing the session |
| ❌ Cloud Run scale-to-zero | — | NOT suitable: needs a live WebSocket 24/7 |

**Golden rules:**
- ⛔ Never delete/lose the store DB → losing it means re-pairing, and repeated pair/unpair is a red flag to Meta.
- ⛔ Do not run 2 processes pointing at the same session → conflict, possible logout.
- ✅ Back up the store DB regularly (it holds encryption keys — protect it like a secret).

---

## 5. Phase 3 — Event loop (replacing the webhook)

Register a handler via `AddEventHandler`; every handler receives **all** events, switching by
type. The standard pattern when the handler needs the Client — wrap it in a struct:

```go
type WAService struct {
    WAClient  *whatsmeow.Client
    Inbox     chan *events.Message // internal queue
}

func (s *WAService) handler(evt any) {
    switch v := evt.(type) {
    case *events.Message:
        // === REQUIRED FILTERS ===
        if v.Info.IsFromMe { return }          // messages you sent yourself (from the phone)
        if v.Info.IsGroup { return }           // skip groups if only handling 1-1 (as needed)
        // Skip history sync: only accept real-time messages
        select {
        case s.Inbox <- v:                     // push to queue, return immediately
        default:
            // queue full → log + metric, do not block the event loop
        }
    case *events.Receipt:
        // delivered / read receipts if tracking is needed
    case *events.Disconnected:
        // metric + log; AutoReconnect will handle it
    case *events.LoggedOut:
        // CRITICAL: session dead, must re-pair → alert immediately
    }
}
```

**Principle:** the handler must **return quickly** — all heavy work (LLM, DB, sending replies,
resolving media) runs in a worker goroutine reading from `Inbox`.

**Extracting incoming message content:**

```go
msg := v.Message
switch {
case msg.GetConversation() != "":            text = msg.GetConversation()
case msg.GetExtendedTextMessage() != nil:    text = msg.GetExtendedTextMessage().GetText()
case msg.GetImageMessage() != nil:           data, _ = client.Download(ctx, msg.GetImageMessage())
case msg.GetAudioMessage() != nil:           data, _ = client.Download(ctx, msg.GetAudioMessage())
// ... DocumentMessage, VideoMessage, StickerMessage similarly
}
```

> The five media sub-structs (`Image/Video/Audio/Document/StickerMessage`) all satisfy the
> `whatsmeow.DownloadableMessage` interface, so `client.Download(ctx, part)` fetches the ciphertext
> from WhatsApp's CDN, decrypts it with the message's `MediaKey`, and verifies `FileSHA256`. You
> cannot HTTP-GET the media URL directly — it is E2E-encrypted.

### Receiving media in this repo

Implemented in `internal/whatsapp/whatsmeow_provider.go` (`handleMessage` → `downloadableMedia`
→ `saveIncomingMedia`):

1. `downloadableMedia` returns the first non-nil media part plus its kind, mimetype, filename
   (documents), and caption; non-media messages are skipped.
2. `saveIncomingMedia` calls `client.Download(ctx, part)` and writes the decrypted bytes under
   `received_media/`.
3. Files are named by WhatsApp message ID (`v.Info.ID`, unique → no collisions) with an extension
   derived from the mimetype; documents keep their original name.

> **Security — path traversal:** the document filename and message ID both come from the *remote
> sender* and are untrusted. `safeBaseName` reduces each to its final path component and rejects
> `.`/`..`/separators, so a document named `../../etc/passwd` cannot escape `received_media/`.

---

## 6. Phase 4 — Dispatcher ("internal webhook")

Goal: **bot logic should not need to know** whether a message came from whatsmeow or the Cloud API.

A worker reads `Inbox` → normalizes it into a payload (recommended: **mimic the Cloud API webhook
format** `entry[].changes[].value.messages[]`) → forwards it:

- **HTTP POST** to the system's existing internal endpoint, or
- **Pub/Sub topic** (already on Google Cloud) for full decoupling, or
- A direct call to the bot logic function if in the same process.

```
events.Message ──> normalize() ──> {from, wamid, type, text|media, ts}
                                        │
                     ┌──────────────────┼──────────────────┐
                     v                  v                  v
              POST /internal      Pub/Sub topic      direct func call
```

**Idempotency:** dedupe by `v.Info.ID` (wamid) before processing — retries/reconnects may replay
events.

---

## 7. Phase 5 — HTTP Send API

An internal endpoint for other services to send messages "on behalf of" your number:

```
POST /send
{
  "to": "8490xxxxxxx",
  "type": "text" | "image" | "audio" | "document",
  "text": "...",              // when type=text
  "media_ref": "gs://bucket/object",  // when type=media → via Media Resolver
  "caption": "..."
}
```

```go
jid := types.NewJID(req.To, types.DefaultUserServer) // number → JID
resp, err := client.SendMessage(ctx, jid, &waE2E.Message{
    Conversation: proto.String(req.Text),
})
```

For media: Resolver (GCS) → check cache by `bucket/object#generation` → `client.Upload()` →
build `waE2E.ImageMessage`/... → `SendMessage`. (Details in the Media Resolver doc.)

**Mandatory security:** this endpoint = permission to message using your personal number.
- API key / IAM / mTLS, only expose inside a private VPC.
- Global rate limiting (`golang.org/x/time/rate`).
- Audit-log every send request.

---

## 8. Phase 6 — Operations & reliability

| Item | How |
|---|---|
| Reconnect | Whatsmeow has built-in AutoReconnect with backoff; still listen to `events.Disconnected` for metrics |
| Logout | `events.LoggedOut` → alert PagerDuty/Slack immediately, requires manual re-pair |
| Health check | A `/healthz` endpoint that reflects `client.IsConnected()` |
| Deploy | **Exactly 1 replica**, restart policy `always`; do NOT scale horizontally |
| Graceful shutdown | Catch SIGTERM → `client.Disconnect()` → flush the queue |
| Expired media | Downloading old messages hits 404/410 → `SendMediaRetryReceipt` asks the phone to re-upload |
| Timeout | `SendMessage` waits 75s for a response by default — separate the context of the GCS-read step from the send step |

---

## 9. Reducing ban risk (IMPORTANT — personal number)

This is an unofficial library that violates WhatsApp's ToS. Every message the bot sends is "you"
messaging. Behavior guidelines:

1. **Reply only, limit initiating:** prioritize replying to incoming messages; do not mass-send to unknown numbers you've never chatted with.
2. **Human-like behavior:** send `client.MarkRead(...)` on receiving a message, `client.SendChatPresence(jid, types.ChatPresenceComposing, ...)` (typing…) 1–3 seconds before replying, and random delays between messages.
3. **Hard rate limit:** cap messages/minute across the whole system, regardless of request source.
4. **Do not pair/unpair repeatedly**, and don't change the server IP erratically (keep a static IP / stable NAT).
5. **Fallback path:** design a `MessageSender` interface with 2 implementations (`WhatsmeowSender`, `CloudAPISender`) — when you need to go official, switch to the Cloud API without changing bot logic.

---

## 10. Deployment checklist

- [ ] Persistent SQL store (SQLite/PVC or Cloud SQL), with backups
- [ ] Pairing successful, session auto-recovers after restart (test: kill process → start → no QR needed)
- [ ] EventHandler filters `IsFromMe`, groups, history sync; pushes to the queue non-blocking
- [ ] Dispatcher normalizes the payload + dedupes by wamid
- [ ] HTTP Send API has auth + rate limiting, only on the internal network
- [ ] Media Resolver GCS plugged into the send flow (cache by generation)
- [ ] Alerts for `events.LoggedOut`; health check based on `IsConnected()`
- [ ] Deploy 1 replica, graceful shutdown, restart always
- [ ] `MessageSender` interface abstracts the backend (whatsmeow ↔ Cloud API)

---

## Appendix — minimal go.mod

```
require (
    go.mau.fi/whatsmeow latest
    github.com/mattn/go-sqlite3 latest        // or jackc/pgx for Postgres
    github.com/mdp/qrterminal/v3 latest       // render the QR to the terminal
    google.golang.org/protobuf latest
    cloud.google.com/go/storage latest        // Media Resolver
)
```

Main packages: `go.mau.fi/whatsmeow`, `go.mau.fi/whatsmeow/store/sqlstore`,
`go.mau.fi/whatsmeow/types`, `go.mau.fi/whatsmeow/types/events`, `go.mau.fi/whatsmeow/proto/waE2E`.

---

*Updated: 2026-07. References: godoc go.mau.fi/whatsmeow; GitHub tulir/whatsmeow (mdtest example,
WhatsApp protocol Q&A in Discussions).*
