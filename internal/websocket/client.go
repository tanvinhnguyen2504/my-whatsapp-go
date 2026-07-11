package websocket

import (
	"context"
	"encoding/json"

	coderws "github.com/coder/websocket"
	"github.com/google/uuid"
)

// Client is a single connected subscriber. Broadcasts are queued on send and
// written to the socket by Serve.
type Client struct {
	ID   string
	conn *coderws.Conn
	send chan Message
}

func NewClient(conn *coderws.Conn) *Client {
	return &Client{
		ID:   uuid.NewString(),
		conn: conn,
		send: make(chan Message, 16),
	}
}

// Serve pumps queued messages to the socket until the client disconnects or ctx
// is canceled. CloseRead drains inbound frames so ping/close are still handled
// on this otherwise write-only connection.
func (c *Client) Serve(ctx context.Context) error {
	ctx = c.conn.CloseRead(ctx)
	for {
		select {
		case <-ctx.Done():
			return c.conn.Close(coderws.StatusNormalClosure, "")
		case m := <-c.send:
			data, err := json.Marshal(m)
			if err != nil {
				continue
			}
			if err := c.conn.Write(ctx, coderws.MessageText, data); err != nil {
				return err
			}
		}
	}
}

// enqueue drops the message when the client's buffer is full, so one slow
// consumer never blocks the broadcast.
func (c *Client) enqueue(m Message) {
	select {
	case c.send <- m:
	default:
	}
}
