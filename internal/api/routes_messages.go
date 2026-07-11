package api

import "github.com/gin-gonic/gin"

func registerMessageRoutes(r gin.IRouter, h *Handler) {
	messages := r.Group("/messages")
	{
		messages.POST("/text", h.sendText)
		messages.POST("/media", h.sendMedia)
		messages.POST("/schedule", h.scheduleText)
	}
}
