package api

import (
	"github.com/gin-gonic/gin"

	"github.com/vinhnguyentan99/my-whatsapp/internal/config"
	"github.com/vinhnguyentan99/my-whatsapp/internal/scheduler"
	"github.com/vinhnguyentan99/my-whatsapp/internal/whatsapp"
)

func NewRouter(cfg config.Config, provider whatsapp.Provider, sched *scheduler.Scheduler) *gin.Engine {
	r := gin.Default()

	h := NewHandler(provider, sched)
	wh := newWebhookHandler(cfg.WebhookVerifyToken)

	// Each domain declares its own routes in a dedicated routes_*.go file.
	registerSystemRoutes(r, h)
	registerMessageRoutes(r, h)
	registerWebhookRoutes(r, wh)

	return r
}
