package api

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"

	"github.com/vinhnguyentan99/my-whatsapp/internal/scheduler"
	"github.com/vinhnguyentan99/my-whatsapp/internal/whatsapp"
)

// Handler holds the dependencies the HTTP routes need.
type Handler struct {
	provider  whatsapp.Provider
	scheduler *scheduler.Scheduler
}

func NewHandler(provider whatsapp.Provider, sched *scheduler.Scheduler) *Handler {
	return &Handler{provider: provider, scheduler: sched}
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"provider": h.provider.Name(),
		"ready":    h.provider.IsReady(),
	})
}

func (h *Handler) qr(c *gin.Context) {
	code := h.provider.QRCode()
	if code == "" {
		c.JSON(http.StatusOK, gin.H{"qr": "", "ready": h.provider.IsReady()})
		return
	}

	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "image/png", png)
}

type sendTextRequest struct {
	To   string `json:"to" binding:"required"`
	Body string `json:"body" binding:"required"`
}

// sendText handles POST /messages/text for whichever provider is active.
func (h *Handler) sendText(c *gin.Context) {
	var req sendTextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.provider.SendText(c.Request.Context(), req.To, req.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) sendMedia(c *gin.Context) {
	to := c.PostForm("to")
	kind := whatsapp.MediaKind(c.PostForm("kind"))
	caption := c.PostForm("caption")

	// type payload struct {
	// 	to      string
	// 	kind    whatsapp.MediaKind
	// 	caption string
	// }

	// test := payload{
	// 	to:      to,
	// 	kind:    kind,
	// 	caption: caption,
	// }

	// fmt.Println("[DEBUG]-test")
	// pkg.DebugJson(test)

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "file is required",
		})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// WhatsApp needs a real mimetype to render the media; fall back to sniffing
	// the bytes when the upload didn't declare one.
	mimetype := file.Header.Get("Content-Type")
	if mimetype == "" || mimetype == "application/octet-stream" {
		mimetype = http.DetectContentType(data)
	}
	if kind == "" {
		kind = kindFromMime(mimetype)
	}

	payload := whatsapp.MediaMessage{
		Kind:     kind,
		Data:     data,
		Mimetype: mimetype,
		FileName: file.Filename,
		Caption:  caption,
	}

	// fmt.Println("[DEBUG]-payload to send the media..")
	// pkg.DebugJson(payload)

	result, err := h.provider.SendMedia(c.Request.Context(), to, payload)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// kindFromMime classifies a MIME type into a WhatsApp media kind. Anything that
// isn't image/video/audio (incl. application/pdf) is treated as a document.
func kindFromMime(mt string) whatsapp.MediaKind {
	switch {
	case strings.HasPrefix(mt, "image/"):
		return whatsapp.MediaImage
	case strings.HasPrefix(mt, "video/"):
		return whatsapp.MediaVideo
	case strings.HasPrefix(mt, "audio/"):
		return whatsapp.MediaAudio
	default:
		return whatsapp.MediaDocument
	}
}

type scheduleTextRequest struct {
	To   string    `json:"to" binding:"required"`
	Body string    `json:"body" binding:"required"`
	At   time.Time `json:"at" binding:"required"` // RFC3339, e.g. "2026-07-07T15:04:05Z"
}

// scheduleText queues a text message to be sent at a future time.
func (h *Handler) scheduleText(c *gin.Context) {
	var req scheduleTextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.scheduler.ScheduleText(req.To, req.Body, req.At)
	c.JSON(http.StatusAccepted, gin.H{"status": "scheduled", "at": req.At})
}
