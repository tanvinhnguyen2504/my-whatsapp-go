package websocket

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// upgrader performs the HTTP -> WebSocket handshake. The default CheckOrigin
// enforces same-origin; to allow a browser served from another origin, set a
// custom CheckOrigin, e.g. func(r *http.Request) bool { return r.Header.Get("Origin") == "https://app.example.com" }.
var upgrader = websocket.Upgrader{}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Stream upgrades the request, registers the client on the hub and blocks
// delivering broadcasts until the client disconnects.
func (h *Handler) Stream(c *gin.Context) {
	// Upgrade writes the 101 response (or an error response) itself; on failure
	// there is nothing left to do.
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	client := NewClient(conn)
	h.svc.Register(client)
	defer h.svc.Unregister(client)

	_ = client.Serve(c.Request.Context(), h.svc.HandleCommand)
}

func (h *Handler) History(c *gin.Context) {
	msgs, err := h.svc.History(c.Request.Context(), c.Param("chat"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, msgs)
}

func (h *Handler) DebugPublish(c *gin.Context) {
	var m Message
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Publish(c.Request.Context(), m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}
