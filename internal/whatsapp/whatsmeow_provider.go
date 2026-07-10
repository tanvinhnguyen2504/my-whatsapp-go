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
)

type WhatsMeowProvider struct {
	postgresDSN string

	client *whatsmeow.Client

	mu     sync.RWMutex
	qrCode string
}

func NewWhatsMeowProvider(postgresDSN string) *WhatsMeowProvider {
	return &WhatsMeowProvider{postgresDSN: postgresDSN}
}

func (p *WhatsMeowProvider) Name() string { return "whatsapp-api" }

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

	// If we manage multi-devices, we need have other way to fetch the device (by ID/)
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

func (p *WhatsMeowProvider) SendMedia(ctx context.Context, to string, m MediaMessage) (SendResult, error) {
	if !p.IsReady() {
		return SendResult{}, fmt.Errorf("provider not ready: log in by scanning the QR code first")
	}

	mediaType, ok := getWhatMeowMediaType(m.Kind)
	if !ok {
		return SendResult{}, fmt.Errorf("unsupported media kind %q", m.Kind)
	}

	jid := p.resolveJID(ctx, to)

	up, err := p.client.Upload(ctx, m.Data, mediaType)
	if err != nil {
		return SendResult{}, fmt.Errorf("upload media: %w", err)
	}

	msg := buildMediaMessage(m, up)

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

func getWhatMeowMediaType(k MediaKind) (whatsmeow.MediaType, bool) {
	switch k {
	case MediaImage:
		return whatsmeow.MediaImage, true
	case MediaVideo:
		return whatsmeow.MediaVideo, true
	case MediaAudio:
		return whatsmeow.MediaAudio, true
	case MediaDocument:
		return whatsmeow.MediaDocument, true
	case MediaSticker:
		return whatsmeow.MediaImage, true // stickers upload with the image media type
	default:
		return "", false
	}
}

func buildMediaMessage(m MediaMessage, up whatsmeow.UploadResponse) *waE2E.Message {
	switch m.Kind {
	case MediaImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(m.Mimetype),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			Caption:       proto.String(m.Caption),
		}}
	case MediaVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(m.Mimetype),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			Caption:       proto.String(m.Caption),
		}}
	case MediaAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(m.Mimetype),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
		}}
	case MediaSticker:
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(m.Mimetype),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
		}}
	default: // MediaDocument
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(m.Mimetype),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			FileName:      proto.String(m.FileName),
			Caption:       proto.String(m.Caption),
		}}
	}
}

func (p *WhatsMeowProvider) eventHandler(rawEvt any) {
	fmt.Println("Case inbound messages...")
	// switch evt := rawEvt.(type) {
	// case *events.Message:
	// 	{
	// 		var messageType, messageContent string
	// 		var mediaMessage *MediaMessage

	// 		messageType = evt.Info.Type
	// 		switch messageType {
	// 		case MessageTypeText:
	// 		case MessageTypeMedia:
	// 			switch evt.Info.MediaType {
	// 			case MessageMediaTypeUrl:
	// 				messageContent = evt.Message.GetExtendedTextMessage().GetText()
	// 			case MessageMediaTypeImage:
	// 				{

	// 				}
	// 			case MessageMediaTypeAudio, MessageMediaTypePtt:
	// 				{

	// 				}
	// 			case MessageMediaTypeVideo, MessageMediaTypeGif:
	// 				{

	// 				}
	// 			case MessageMediaTypeDocument:
	// 				{

	// 				}
	// 			case MessageMediaTypeSticker, MessageMediaType1pSticker:
	// 				{

	// 				}
	// 				// do nothing for other media types
	// 			}
	// 		case MessageTypePoll, MessageTypeReaction:
	// 			// do nothing...
	// 		}
	// 	}
	// case *events.Connected:
	// 	slog.Info("whatsapp connected")
	// case *events.LoggedOut:
	// 	slog.Warn("whatsapp logged out; QR re-pairing required")
	// }
}

const receivedMediaDir = "received_media"

func (p *WhatsMeowProvider) handleMessage(e *events.Message) {
	if text := e.Message.GetConversation(); text != "" {
		slog.Info("incoming text",
			"from", e.Info.Sender.String(),
			"chat", e.Info.Chat.String(),
			"text", text,
		)
		return
	}

	fmt.Println(e.Message, "e.Message...")

	media, ok := downloadableMedia(e.Message)
	if !ok {
		return
	}

	path, err := p.saveIncomingMedia(context.Background(), e.Info.ID, media)
	if err != nil {
		slog.Error("save incoming media", "from", e.Info.Sender.String(), "error", err)
		return
	}
	slog.Info("saved incoming media",
		"from", e.Info.Sender.String(),
		"chat", e.Info.Chat.String(),
		"kind", media.kind,
		"mimetype", media.mimetype,
		"caption", media.caption,
		"path", path,
	)
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

// saveIncomingMedia downloads (decrypts) the attachment and writes it under
// receivedMediaDir, returning the file path.
func (p *WhatsMeowProvider) saveIncomingMedia(ctx context.Context, msgID string, media IncomingMedia) (string, error) {
	data, err := p.client.Download(ctx, media.msg)
	if err != nil {
		return "", fmt.Errorf("download media: %w", err)
	}

	if err := os.MkdirAll(receivedMediaDir, 0o755); err != nil {
		return "", fmt.Errorf("create media dir: %w", err)
	}

	path := filepath.Join(receivedMediaDir, incomingFileName(msgID, media))
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
