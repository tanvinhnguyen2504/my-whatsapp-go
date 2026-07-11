package websocket

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client is a single connected subscriber. Outbound events are queued on send
// and written to the socket by the Serve write loop.
type Client struct {
	ID   string
	conn *websocket.Conn
	send chan ServerEvent
}

func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		ID:   uuid.NewString(),
		conn: conn,
		send: make(chan ServerEvent, 16),
	}
}

// Serve runs the connection bidirectionally until the client disconnects or ctx
// is canceled: a read loop dispatches client commands via onCommand and queues
// the resulting ack, while the write loop drains queued events to the socket.
//
// gorilla's ReadMessage/WriteMessage are not context-aware, so the write loop's
// deferred Close is what unblocks the blocked reader on shutdown (Close is the
// one method safe to call concurrently with a read).
func (c *Client) Serve(ctx context.Context, onCommand func(context.Context, ClientCommand) ServerEvent) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.conn.Close()

	// Read loop (sole reader): exits on disconnect/error and cancels ctx so the
	// write loop stops too.
	go func() {
		defer cancel()
		for {
			_, data, err := c.conn.ReadMessage()
			if err != nil {
				return
			}
			var cmd ClientCommand
			if err := json.Unmarshal(data, &cmd); err != nil {
				continue // ignore malformed frames
			}
			c.enqueue(onCommand(ctx, cmd))
		}
	}()

	// Write loop (sole writer): returning here closes the conn (deferred), which
	// unblocks the read goroutine.
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-c.send:
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return err
			}
		}
	}
}

// enqueue drops the event when the client's buffer is full, so one slow consumer
// never blocks a broadcast.
func (c *Client) enqueue(ev ServerEvent) {
	select {
	case c.send <- ev:
	default:
	}
}
