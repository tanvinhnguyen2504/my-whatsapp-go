# WebSocket concepts — a guide to `internal/websocket`

A plain-language explanation of the building blocks used in the real-time layer:
**Client, Hub, Broadcast, the send channel, Serve (read/write loops), Service, ack,
idempotency**, and how they fit together. For the end-to-end flows see
`realtime_chat_workflow.md`.

## 1. Why a WebSocket at all?

HTTP is **request → response**: the client asks, the server answers, the connection is done.
That's perfect for "send this message" but useless for "tell me the instant a new message
arrives" — the server has no way to speak first.

A **WebSocket** is a single long-lived, two-way connection. After an initial HTTP handshake
("Upgrade"), either side can send bytes ("frames") at any time. That's what lets the server
**push** an incoming WhatsApp message to the browser the moment it arrives.

```
HTTP:       client ──ask──▶ server ──answer──▶ client   (then closed)
WebSocket:  client ◀───────one open pipe───────▶ server  (both can speak, stays open)
```

## 2. The glossary (one line each)

| Term | What it is | In the code |
|------|-----------|-------------|
| **Upgrade** | Turning an HTTP request into a WebSocket connection | `upgrader.Upgrade` in `handler.go` |
| **Client** | One connected browser: its socket + a personal outbox | `client.go` `Client` |
| **send channel** | A per-client queue of outgoing frames | `Client.send chan ServerEvent` |
| **enqueue** | Put a frame on a client's queue (drop if full) | `Client.enqueue` |
| **Hub** | The registry of all connected clients | `hub.go` `Hub` |
| **Broadcast** | Fan a message out to every client's queue | `Hub.Broadcast` |
| **Serve** | The per-client read + write loops | `Client.Serve` |
| **Service** | Coordinator: broadcast, persist, dispatch sends | `service.go` `Service` |
| **Repo** | Postgres persistence of messages | `repo.go` `MessageRepo` |
| **Handler** | HTTP entrypoints that mount the socket | `handler.go` `Handler` |
| **Frame / envelope** | One typed JSON message on the wire | `Message`, `ServerEvent`, `ClientCommand` |
| **ack** | Server's reply confirming a client's send | `ServerEvent{Type:"ack"}` |
| **req_id** | Correlation id tying an ack to its send | field on `ClientCommand` / ack |
| **idempotency** | Making a retry safe (no double-send) | `Service.HandleCommand` / `reserve` |

## 3. Client — one connected browser

```go
type Client struct {
	ID   string
	conn *websocket.Conn    // the raw socket (gorilla/websocket)
	send chan ServerEvent   // this client's personal outbox (buffered, size 16)
}
```

Each browser that connects becomes one `Client`. Two things matter:

- **`conn`** is the actual network socket. Only **one goroutine may write** to it at a time.
- **`send`** is a **buffered channel** — an in-memory queue of frames waiting to go out. This
  is the single most important design choice, explained next.

## 4. The send channel + enqueue — decoupling and backpressure

When a new WhatsApp message arrives, we want to hand it to every client **immediately** and
move on. We do *not* want to block on the network write to a slow browser. The `send` channel
makes that possible:

```go
func (c *Client) enqueue(ev ServerEvent) {
	select {
	case c.send <- ev: // put it on the queue
	default:           // queue full → drop it (don't block the producer)
	}
}
```

- **Decoupling**: producing a message (`enqueue`) is separated from delivering it
  (the write loop drains `send`). The producer never waits for the socket.
- **Backpressure via drop**: the channel holds up to 16 frames. If a browser is so slow that
  its queue fills, `enqueue` takes the `default` branch and **drops** the frame rather than
  freezing everyone. (A stricter system might disconnect the slow client instead; dropping is
  the simple choice here.)

`★ Mental model:` think of each client as having a small mailbox. Broadcasting = dropping a
copy in every mailbox. A courier (the write loop) empties each mailbox onto the wire at its
own pace. A jammed mailbox doesn't hold up the others.

## 5. Hub — the registry of clients

The **Hub** is just "the set of everyone currently connected," protected by a lock because
clients connect and disconnect from different goroutines concurrently.

```go
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client   // clientID -> Client
}
```

- `Register(c)` — add a client when its socket opens.
- `Unregister(c)` — remove it when the socket closes.
- `Broadcast(m)` — see below.

The `sync.RWMutex` allows many concurrent readers (broadcasts) but exclusive writers
(register/unregister), so the map is never mutated while being iterated.

## 6. Broadcast — one message to everyone

```go
func (h *Hub) Broadcast(m Message) {
	ev := ServerEvent{Type: "message", Data: &m} // wrap in the envelope once
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		c.enqueue(ev) // drop a copy in each client's mailbox
	}
}
```

**Broadcast = fan-out.** It loops over every registered client and `enqueue`s the same event.
Because `enqueue` is non-blocking, `Broadcast` returns quickly no matter how many (or how
slow) the clients are. Note it wraps the raw `Message` into a `ServerEvent{Type:"message"}`
envelope so the browser can tell inbound messages apart from acks.

## 7. Serve — the read loop and write loop

`Serve` is what actually runs a connection. It runs **two loops at once**:

```go
func (c *Client) Serve(ctx, onCommand) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer c.conn.Close()     // closing here unblocks the blocked reader below

	go func() {              // READ loop (one goroutine)
		defer cancel()
		for {
			_, data, err := c.conn.ReadMessage() // wait for a frame FROM the browser
			if err != nil { return }             // socket closed → stop everything
			var cmd ClientCommand
			json.Unmarshal(data, &cmd)
			c.enqueue(onCommand(ctx, cmd))       // handle it, queue the ack
		}
	}()

	for {                    // WRITE loop (this goroutine — the sole writer)
		select {
		case <-ctx.Done():
			return nil       // deferred Close() then unblocks the reader
		case ev := <-c.send:                     // take a frame from the mailbox
			data, _ := json.Marshal(ev)
			c.conn.WriteMessage(websocket.TextMessage, data) // send it TO the browser
		}
	}
}
```

- **Write loop** — the *only* place that writes to the socket. It blocks on `c.send` and
  writes whatever shows up (broadcasts and acks alike). gorilla requires a **single writer
  goroutine** — this is it.
- **Read loop** — waits for frames *from* the browser (send commands), dispatches each via
  `onCommand` (which returns an ack), and queues the ack back through the same mailbox.
  Continuously reading also lets gorilla handle **ping/pong/close** control frames for us.
- **Lifecycle**: `gorilla`'s `ReadMessage`/`WriteMessage` are **not** context-aware, so we
  bridge `context` to the socket manually. If the browser disconnects, `ReadMessage` errors
  and `cancel()` fires → the write loop's `<-ctx.Done()` returns → the **deferred `Close()`**
  runs, which in turn unblocks any still-blocked `ReadMessage`. Either loop ending tears down
  the other. (gorilla allows one concurrent reader + one concurrent writer, and `Close` is safe
  to call concurrently with them — which is what makes this teardown work.)

> This project uses **`github.com/gorilla/websocket`**. Some libraries (e.g. `coder/websocket`)
> take a `context` on every read/write and offer a `CloseRead` helper for receive-only
> connections; gorilla does neither, so the lifecycle is managed with the `context` +
> deferred-`Close` pattern above.

## 8. Service — the coordinator

The **Hub** only knows how to fan out. The **Service** is the brain that ties the hub, the
database, and the provider together:

```go
type Service struct {
	hub  *Hub
	repo *MessageRepo
	send SendFunc          // how to actually send a WhatsApp message
	...
}
```

Two entry points:

- **`Publish(msg)`** — for an *inbound* message: persist it (`repo.Insert`) then
  `hub.Broadcast`. This is the receive path.
- **`HandleCommand(cmd)`** — for an *outbound* send from a browser: call `send(...)`
  (the provider) and return an ack. This is the send path.

`SendFunc` is a small function type the Service is given at construction, so the websocket
package never imports the WhatsApp provider directly (clean layering).

## 9. ack, req_id, and idempotency

A WebSocket `send` is **fire-and-forget** — the browser gets no automatic reply. So the server
sends an explicit **acknowledgement**:

```
browser ──▶ { "type":"send", "req_id":"abc", "to":"84…", "body":"hi" }
server  ──▶ { "type":"ack",  "req_id":"abc", "ok":true, "message_id":"3EB0…" }
```

- **`req_id`** is a **correlation id**. The one socket carries many messages mixed together;
  `req_id` lets the browser match an ack back to the exact `send` that's waiting for it
  (it keeps a `Map<req_id, pending>`).
- **Idempotency** makes **retries safe**. If an ack is lost (socket dropped after the server
  already sent), the browser retries with the **same `req_id`**. The Service remembers each
  `req_id` and returns the *original* ack instead of sending again:

```go
entry, first := s.reserve(cmd.ReqID) // first caller sends; duplicates wait & reuse
if !first {
	<-entry.done
	return entry.ev   // same ack, no second send
}
```

Without this, "retry after a lost ack" would deliver the WhatsApp message twice.

## 10. How the pieces fit — the two flows

**Receive (WhatsApp → browsers):**
```
provider event → Service.Publish → repo.Insert (save) + Hub.Broadcast (fan-out)
   → each Client.enqueue → Client write loop → conn.Write → browser
```

**Send (browser → WhatsApp):**
```
browser conn.Write → Client read loop → Service.HandleCommand → SendFunc → provider.SendText
   → ack → Client.enqueue → write loop → conn.Write → browser
```

## 11. Concurrency model at a glance

| Primitive | Used for | Where |
|-----------|----------|-------|
| **goroutine** | one read loop + one write loop per client | `Client.Serve` |
| **buffered channel** | per-client outbound queue (decouple + backpressure) | `Client.send` |
| **`context`** | tie the two loops' lifetimes; cancel on disconnect | `Client.Serve` |
| **`sync.RWMutex`** | protect the client registry during concurrent connect/broadcast | `Hub.mu` |
| **`sync.Mutex` + map** | the idempotency cache | `Service.mu` / `acks` |

The rules that keep it safe: **one writer per socket** (the write loop), **the hub map is only
touched under its lock**, and **producers never block** (they `enqueue`, which drops rather
than waits).

## 12. Quick reference

- **Client** = one browser (socket + mailbox).
- **send channel** = that browser's mailbox (buffered queue).
- **enqueue** = drop a frame in a mailbox, non-blocking.
- **Hub** = the set of all mailboxes.
- **Broadcast** = drop a copy in every mailbox.
- **Serve** = the read loop (in) + write loop (out) that run a connection.
- **Service** = coordinator: `Publish` (receive) and `HandleCommand` (send).
- **ack** = the server's reply to a send; **req_id** correlates it; **idempotency** makes
  retries safe.
