package api

import "github.com/gin-gonic/gin"

func registerSystemRoutes(r gin.IRouter, h *Handler) {
	r.GET("/health", h.health)
	r.GET("/qr", h.qr)
}
