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

	r.GET("/health", h.health)
	r.GET("/qr", h.qr)

	messages := r.Group("/messages")
	{
		messages.POST("/text", h.sendText)
		messages.POST("/media", h.sendMedia)
		messages.POST("/schedule", h.scheduleText)
	}

	// For the API
	webhook := r.Group("/webhook")
	{
		webhook.GET("", wh.verify)   // Meta verification handshake
		webhook.POST("", wh.receive) // inbound events
	}

	return r
}
