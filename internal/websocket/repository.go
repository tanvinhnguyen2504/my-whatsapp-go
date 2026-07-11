package websocket

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// MessageRepo persists inbound messages to PostgreSQL for history.
type MessageRepo struct {
	db *sqlx.DB
}

func NewMessageRepo(db *sqlx.DB) *MessageRepo {
	return &MessageRepo{db: db}
}

const schemaMessages = `
CREATE TABLE IF NOT EXISTS ws_messages (
	id         TEXT PRIMARY KEY,
	chat_jid   TEXT NOT NULL,
	sender_jid TEXT NOT NULL,
	kind       TEXT NOT NULL,
	body       TEXT NOT NULL DEFAULT '',
	media_path TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS ws_messages_chat_idx ON ws_messages (chat_jid, created_at DESC);`

// EnsureSchema creates the messages table if it does not exist.
func (r *MessageRepo) EnsureSchema(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, schemaMessages)
	return err
}

// Insert stores a message, ignoring duplicates so re-delivered events are safe.
func (r *MessageRepo) Insert(ctx context.Context, m Message) error {
	const q = `
INSERT INTO ws_messages (id, chat_jid, sender_jid, kind, body, media_path, created_at)
VALUES (:id, :chat_jid, :sender_jid, :kind, :body, :media_path, :created_at)
ON CONFLICT (id) DO NOTHING`
	_, err := r.db.NamedExecContext(ctx, q, m)
	return err
}

func (r *MessageRepo) List(ctx context.Context, chatJID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, chat_jid, sender_jid, kind, body, media_path, created_at
FROM ws_messages WHERE chat_jid = $1
ORDER BY created_at DESC LIMIT $2`
	var out []Message
	err := r.db.SelectContext(ctx, &out, q, chatJID, limit)
	return out, err
}
