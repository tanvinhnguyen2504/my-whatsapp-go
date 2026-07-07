package api

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// webhookHandler serves the Meta WhatsApp Business Cloud API webhook. It is only
// meaningful for the Business workflow; the WhatsMeow workflow receives inbound
// messages via its in-process event handler instead.
type webhookHandler struct {
	verifyToken string
}

func newWebhookHandler(verifyToken string) *webhookHandler {
	return &webhookHandler{verifyToken: verifyToken}
}

// verify handles Meta's GET verification handshake: echo hub.challenge back when
// the mode and verify token match.
func (w *webhookHandler) verify(c *gin.Context) {
	mode := c.Query("hub.mode")
	token := c.Query("hub.verify_token")
	challenge := c.Query("hub.challenge")

	if mode == "subscribe" && token == w.verifyToken && w.verifyToken != "" {
		c.String(http.StatusOK, challenge)
		return
	}
	c.Status(http.StatusForbidden)
}

// receive handles inbound event notifications (messages, statuses). The skeleton
// logs the raw payload; parse and dispatch it as you develop the Business flow.
func (w *webhookHandler) receive(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	slog.Info("webhook event received", "payload", string(body))
	c.Status(http.StatusOK)
}
