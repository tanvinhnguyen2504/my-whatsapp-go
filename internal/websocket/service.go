package websocket

import "context"

type Service struct {
	hub  *Hub
	repo *MessageRepo
}

func NewService(hub *Hub, repo *MessageRepo) *Service {
	return &Service{hub: hub, repo: repo}
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
