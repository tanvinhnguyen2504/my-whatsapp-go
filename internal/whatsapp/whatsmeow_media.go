package whatsapp

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

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
