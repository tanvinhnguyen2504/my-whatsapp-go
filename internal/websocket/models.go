package websocket

import (
	"context"
	"time"
)

// Message is an inbound WhatsApp message. The same value is both broadcast to
// WebSocket clients (json tags) and persisted for history (db tags).
type Message struct {
	ID        string    `json:"id" db:"id"`
	ChatJID   string    `json:"chat_jid" db:"chat_jid"`
	SenderJID string    `json:"sender_jid" db:"sender_jid"`
	Kind      string    `json:"kind" db:"kind"` // text | image | video | audio | document | sticker
	Body      string    `json:"body" db:"body"` // text content or media caption
	MediaPath string    `json:"media_path,omitempty" db:"media_path"`
	Timestamp time.Time `json:"timestamp" db:"created_at"`
}

// ClientCommand is a frame sent by a browser over the socket.
type ClientCommand struct {
	Type  string `json:"type"`   // "send"
	ReqID string `json:"req_id"` // client-generated, reused across retries for idempotency
	To    string `json:"to"`
	Body  string `json:"body"`
}

// ServerEvent is a frame the server sends to a client. Type discriminates the
// payload: "message" carries Data (an inbound broadcast); "ack" carries the
// result of a client's send command, correlated by ReqID.
type ServerEvent struct {
	Type      string   `json:"type"` // "message" | "ack"
	Data      *Message `json:"data,omitempty"`
	ReqID     string   `json:"req_id,omitempty"`
	OK        bool     `json:"ok,omitempty"`
	MessageID string   `json:"message_id,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// SendFunc sends a text message through the active provider, returning the
// provider's message id. Supplied by main so the websocket layer does not depend
// on the whatsapp package.
type SendFunc func(ctx context.Context, to, body string) (messageID string, err error)
