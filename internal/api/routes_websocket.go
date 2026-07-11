package api

import (
	"github.com/gin-gonic/gin"

	"github.com/vinhnguyentan99/my-whatsapp/internal/websocket"
)

func registerWebsocketRoutes(r gin.IRouter, h *websocket.Handler) {
	r.GET("/ws", h.Stream)
	r.GET("/ws/history/:chat", h.History)
	r.POST("/ws/publish", h.DebugPublish) // dev-only: inject a message to test broadcast
}
