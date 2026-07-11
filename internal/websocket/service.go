package websocket

import (
	"context"
	"fmt"
	"sync"
)

type Service struct {
	hub  *Hub
	repo *MessageRepo
	send SendFunc

	mu   sync.Mutex
	acks map[string]*ackEntry // reqID -> ack (idempotency); grows for process lifetime
}

// ackEntry lets a retried command reuse the original ack: the first caller sends
// and closes done; duplicates wait on done and return the same ev.
type ackEntry struct {
	done chan struct{}
	ev   ServerEvent
}

func NewService(hub *Hub, repo *MessageRepo, send SendFunc) *Service {
	return &Service{hub: hub, repo: repo, send: send, acks: make(map[string]*ackEntry)}
}

func (s *Service) Publish(ctx context.Context, m Message) error {
	if err := s.repo.Insert(ctx, m); err != nil {
		return err
	}
	s.hub.Broadcast(m)
	return nil
}

func (s *Service) Register(c *Client) {
	s.hub.Register(c)
}

func (s *Service) Unregister(c *Client) {
	s.hub.Unregister(c)
}

func (s *Service) History(ctx context.Context, chatJID string, limit int) ([]Message, error) {
	return s.repo.List(ctx, chatJID, limit)
}

// HandleCommand dispatches a client command and returns the ack to send back.
// A retried command (same ReqID) reuses the original ack instead of sending
// again, so a lost ack never double-sends the WhatsApp message.
func (s *Service) HandleCommand(ctx context.Context, cmd ClientCommand) ServerEvent {
	if cmd.Type != "send" {
		return ServerEvent{Type: "ack", ReqID: cmd.ReqID, Error: fmt.Sprintf("unknown command %q", cmd.Type)}
	}

	entry, first := s.reserve(cmd.ReqID)
	if !first {
		select {
		case <-entry.done:
			return entry.ev
		case <-ctx.Done():
			return ServerEvent{Type: "ack", ReqID: cmd.ReqID, Error: "canceled"}
		}
	}

	ack := ServerEvent{Type: "ack", ReqID: cmd.ReqID}
	id, err := s.send(ctx, cmd.To, cmd.Body)
	if err != nil {
		ack.Error = err.Error()
	} else {
		ack.OK = true
		ack.MessageID = id
	}

	entry.ev = ack
	close(entry.done)
	return ack
}

// reserve returns the ack entry for reqID, creating it if absent. first is true
// for the caller that must perform the send. An empty reqID is never deduped.
func (s *Service) reserve(reqID string) (*ackEntry, bool) {
	if reqID == "" {
		return &ackEntry{done: make(chan struct{})}, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.acks[reqID]; ok {
		return e, false
	}
	e := &ackEntry{done: make(chan struct{})}
	s.acks[reqID] = e
	return e, true
}
