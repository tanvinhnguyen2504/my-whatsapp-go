// Package scheduler runs messages at a future time. The skeleton keeps jobs in
// memory with time.AfterFunc; swap in a persistent store when durability matters.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/vinhnguyentan99/my-whatsapp/internal/whatsapp"
)

type Scheduler struct {
	provider whatsapp.Provider
}

func New(provider whatsapp.Provider) *Scheduler {
	return &Scheduler{provider: provider}
}

// ScheduleText sends a text message once, at the given time. If at is in the
// past the message is sent almost immediately.
func (s *Scheduler) ScheduleText(to, body string, at time.Time) {
	delay := time.Until(at)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		if _, err := s.provider.SendText(context.Background(), to, body); err != nil {
			slog.Error("scheduled send failed", "to", to, "error", err)
		}
	})
}
