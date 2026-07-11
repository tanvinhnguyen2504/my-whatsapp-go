package api

import (
	"github.com/gin-gonic/gin"

	"github.com/vinhnguyentan99/my-whatsapp/internal/config"
	"github.com/vinhnguyentan99/my-whatsapp/internal/scheduler"
	"github.com/vinhnguyentan99/my-whatsapp/internal/websocket"
	"github.com/vinhnguyentan99/my-whatsapp/internal/whatsapp"
)

func NewRouter(cfg config.Config, provider whatsapp.Provider, sched *scheduler.Scheduler, wsSvc *websocket.Service) *gin.Engine {
	r := gin.Default()

	h := NewHandler(provider, sched)
	wh := newWebhookHandler(cfg.WebhookVerifyToken)
	wsH := websocket.NewHandler(wsSvc)

	// Each domain declares its own routes in a dedicated routes_*.go file.
	registerSystemRoutes(r, h)
	registerMessageRoutes(r, h)
	registerWebhookRoutes(r, wh)
	registerWebsocketRoutes(r, wsH)

	return r
}
