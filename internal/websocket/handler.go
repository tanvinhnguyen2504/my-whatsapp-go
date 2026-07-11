package websocket

import (
	"net/http"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// Handler upgrades HTTP connections to WebSocket and streams inbound WhatsApp
// messages to the client through the Service hub.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Stream upgrades the request, registers the client on the hub and blocks
// delivering broadcasts until the client disconnects.
func (h *Handler) Stream(c *gin.Context) {
	// Origin is verified (same-origin) by default. To allow a browser frontend
	// served from another origin, list it here, e.g. OriginPatterns: []string{"app.example.com"}.
	conn, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{})
	if err != nil {
		return
	}

	client := NewClient(conn)
	h.svc.Register(client)
	defer h.svc.Unregister(client)

	_ = client.Serve(c.Request.Context())
}

// History returns the most recent stored messages for a chat JID.
func (h *Handler) History(c *gin.Context) {
	msgs, err := h.svc.History(c.Request.Context(), c.Param("chat"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, msgs)
}
