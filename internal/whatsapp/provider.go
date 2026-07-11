// Package whatsapp defines the provider abstraction shared by both WhatsApp
// workflows (the unofficial WhatsMeow personal account and the official Meta
// Business Cloud API) and their concrete implementations.
package whatsapp

import (
	"context"
	"time"
)

// SendResult is returned after a message is accepted by the underlying provider.
type SendResult struct {
	MessageID string `json:"message_id"`
	Provider  string `json:"provider"`
}

// InboundMessage is a received WhatsApp message handed to an optional sink so a
// caller (e.g. the WebSocket layer) can broadcast/persist it, without the
// provider depending on that layer.
type InboundMessage struct {
	ID        string
	ChatJID   string
	SenderJID string
	Kind      string // text | image | video | audio | document | sticker
	Body      string // text content or media caption
	MediaPath string // local path for saved media, if any
	Timestamp time.Time
}

// InboundFunc consumes received messages. It may be nil (no sink).
type InboundFunc func(context.Context, InboundMessage)

// MediaKind classifies an outgoing media message.
type MediaKind string

const (
	MediaImage    MediaKind = "image"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaDocument MediaKind = "document"
	MediaSticker  MediaKind = "sticker" // WebP image sent as a sticker
)

type MediaMessage struct {
	Kind     MediaKind
	Data     []byte
	Mimetype string
	FileName string // used for documents
	Caption  string // used for image/video/document
}

type Provider interface {
	Name() string
	Connect(ctx context.Context) error
	Disconnect()
	IsReady() bool
	QRCode() string
	SendText(ctx context.Context, to, body string) (SendResult, error)
	SendMedia(ctx context.Context, to string, m MediaMessage) (SendResult, error)
}
