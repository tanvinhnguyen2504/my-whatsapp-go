package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/vinhnguyentan99/my-whatsapp/pkg"
)

type WhatsMeowProvider struct {
	postgresDSN string
	schema      string
	onInbound   InboundFunc

	client *whatsmeow.Client

	mu     sync.RWMutex
	qrCode string
}

const RECEIVED_MEDIA_DIR = "received_media"

func NewWhatsMeowProvider(postgresDSN string, onInbound InboundFunc) *WhatsMeowProvider {
	return &WhatsMeowProvider{postgresDSN: postgresDSN, onInbound: onInbound}
}

func (p *WhatsMeowProvider) Name() string { return "whatsapp-business" }

func (p *WhatsMeowProvider) Connect(ctx context.Context) error {
	dbLog := waLog.Stdout("Database", "WARN", true)

	db, err := sql.Open("pgx", p.postgresDSN)
	if err != nil {
		return fmt.Errorf("open postgres store: %w", err)
	}

	container := sqlstore.NewWithDB(db, "postgres", dbLog)
	if err := container.Upgrade(ctx); err != nil {
		return fmt.Errorf("upgrade store schema: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("load device: %w", err)
	}

	p.client = whatsmeow.NewClient(device, waLog.Stdout("Client", "INFO", true))
	p.client.AddEventHandler(p.eventHandler)

	if p.client.Store.ID == nil {
		return p.pairAndConnect(ctx)
	}
	if err := p.client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}

func (p *WhatsMeowProvider) pairAndConnect(ctx context.Context) error {
	qrChan, err := p.client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get qr channel: %w", err)
	}
	if err := p.client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	go func() {
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				p.setQR(evt.Code)
				slog.Info("scan whatsapp qr to log in", "qr", evt.Code)
			case "success":
				p.setQR("")
				slog.Info("whatsapp login successful")
			default:
				p.setQR("")
				slog.Warn("whatsapp qr event", "event", evt.Event, "error", evt.Error)
			}
		}
	}()
	return nil
}

// quoteIdentifier double-quotes a Postgres identifier so a schema name from
// config can be interpolated into DDL safely.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (p *WhatsMeowProvider) Disconnect() {
	if p.client != nil {
		p.client.Disconnect()
	}
}

func (p *WhatsMeowProvider) IsReady() bool {
	return p.client != nil && p.client.IsConnected() && p.client.IsLoggedIn()
}

func (p *WhatsMeowProvider) QRCode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.qrCode
}

func (p *WhatsMeowProvider) SendText(ctx context.Context, to, body string) (SendResult, error) {
	if !p.IsReady() {
		return SendResult{}, fmt.Errorf("provider not ready: log in by scanning the QR code first")
	}
	jid := p.resolveJID(ctx, to)
	msg := &waE2E.Message{Conversation: proto.String(body)}

	resp, err := p.client.SendMessage(ctx, jid, msg)
	if err != nil {
		return SendResult{}, fmt.Errorf("send message: %w", err)
	}
	return SendResult{MessageID: resp.ID, Provider: p.Name()}, nil
}

// resolveJID resolves the recipient's WhatsApp JID for `to` (E.164 digits, no '+').
//
// It resolves + persists the recipient's LID via GetUserInfo (which calls
// PutManyLIDMappings). This matters for never-contacted numbers: WhatsApp requires a
// privacy token (cstoken) derived from the recipient's LID on the first message to a
// cold contact, and WhatsMeow can only generate it once the phone<->LID mapping is in
// the store — otherwise the server rejects the send with error 463
// (NackCallerReachoutTimelocked). Best-effort: falls back to the phone JID.
func (p *WhatsMeowProvider) resolveJID(ctx context.Context, to string) types.JID {
	pnJID := types.NewJID(to, types.DefaultUserServer)
	// if info, err := p.client.GetUserInfo(ctx, []types.JID{pnJID}); err == nil {
	// 	for _, u := range info { // iterate to avoid response-key format mismatches
	// 		if !u.LID.IsEmpty() {
	// 			return u.LID
	// 		}
	// 	}
	// }
	return pnJID
}

func (p *WhatsMeowProvider) eventHandler(rawEvt any) {
	fmt.Println("[DEBUG]-eventHandler...")
	// pkg.DebugJson(rawEvt)
	switch evt := rawEvt.(type) {
	case *events.Message:
		p.handleInbound(evt)
	case *events.Connected:
		slog.Info("whatsapp connected")
	case *events.LoggedOut:
		slog.Warn("whatsapp logged out; QR re-pairing required")
		// case *events.PairSuccess:
		// 	slog.Warn()
	}
}

func (p *WhatsMeowProvider) handleInbound(e *events.Message) {
	if p.onInbound == nil {
		return
	}

	msg := InboundMessage{
		ID:        e.Info.ID,
		ChatJID:   e.Info.Chat.String(),
		SenderJID: e.Info.Sender.String(),
		Timestamp: e.Info.Timestamp,
	}

	fmt.Println("[DEBUG]-inbound message")
	pkg.DebugJson(msg)

	switch {
	case e.Message.GetConversation() != "":
		msg.Kind, msg.Body = "text", e.Message.GetConversation()
	case e.Message.GetExtendedTextMessage().GetText() != "":
		msg.Kind, msg.Body = "text", e.Message.GetExtendedTextMessage().GetText()
	default:
		media, ok := downloadableMedia(e.Message)
		if !ok {
			return
		}
		msg.Kind, msg.Body = string(media.kind), media.caption
		if path, err := p.saveIncomingMedia(context.Background(), e.Info.ID, media); err == nil {
			msg.MediaPath = path
		} else {
			slog.Error("save incoming media", "from", msg.SenderJID, "error", err)
		}
	}

	p.onInbound(context.Background(), msg)
}

func downloadableMedia(m *waE2E.Message) (IncomingMedia, bool) {
	switch {
	case m.GetImageMessage() != nil:
		im := m.GetImageMessage()
		return IncomingMedia{im, MediaImage, im.GetMimetype(), "", im.GetCaption()}, true
	case m.GetVideoMessage() != nil:
		vm := m.GetVideoMessage()
		return IncomingMedia{vm, MediaVideo, vm.GetMimetype(), "", vm.GetCaption()}, true
	case m.GetAudioMessage() != nil:
		am := m.GetAudioMessage()
		return IncomingMedia{am, MediaAudio, am.GetMimetype(), "", ""}, true
	case m.GetDocumentMessage() != nil:
		dm := m.GetDocumentMessage()
		return IncomingMedia{dm, MediaDocument, dm.GetMimetype(), dm.GetFileName(), dm.GetCaption()}, true
	case m.GetStickerMessage() != nil:
		sm := m.GetStickerMessage()
		return IncomingMedia{sm, MediaSticker, sm.GetMimetype(), "", ""}, true
	default:
		return IncomingMedia{}, false
	}
}

func (p *WhatsMeowProvider) saveIncomingMedia(ctx context.Context, msgID string, media IncomingMedia) (string, error) {
	data, err := p.client.Download(ctx, media.msg)
	if err != nil {
		return "", fmt.Errorf("download media: %w", err)
	}

	if err := os.MkdirAll(RECEIVED_MEDIA_DIR, 0o755); err != nil {
		return "", fmt.Errorf("create media dir: %w", err)
	}

	path := filepath.Join(RECEIVED_MEDIA_DIR, incomingFileName(msgID, media))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write media file: %w", err)
	}
	return path, nil
}

func incomingFileName(msgID string, media IncomingMedia) string {
	id := safeBaseName(msgID)
	if id == "" {
		id = "message"
	}
	if name := safeBaseName(media.filename); name != "" {
		return id + "_" + name
	}
	return id + extensionForMime(media.mimetype)
}

// safeBaseName reduces an untrusted name to its final path component and drops it
// entirely if it still resolves to a traversal token or contains a separator.
func safeBaseName(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(name))
	if base == "." || base == ".." || strings.ContainsAny(base, `/\`) {
		return ""
	}
	return base
}

// extensionForMime maps a mimetype ("image/jpeg", "audio/ogg; codecs=opus") to a
// file extension, falling back to ".bin" when it is unknown.
func extensionForMime(mimetype string) string {
	if i := strings.IndexByte(mimetype, ';'); i >= 0 {
		mimetype = strings.TrimSpace(mimetype[:i])
	}
	if exts, err := mime.ExtensionsByType(mimetype); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func (p *WhatsMeowProvider) setQR(code string) {
	p.mu.Lock()
	p.qrCode = code
	p.mu.Unlock()
}
