package api

import "github.com/gin-gonic/gin"

func registerWebhookRoutes(r gin.IRouter, wh *webhookHandler) {
	webhook := r.Group("/webhook")
	{
		webhook.GET("", wh.verify)   // Meta verification handshake
		webhook.POST("", wh.receive) // inbound events
	}
}
