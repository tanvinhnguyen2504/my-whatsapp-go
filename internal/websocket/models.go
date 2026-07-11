package websocket

import "time"

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
