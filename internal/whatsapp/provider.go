// Package whatsapp defines the provider abstraction shared by both WhatsApp
// workflows (the unofficial WhatsMeow personal account and the official Meta
// Business Cloud API) and their concrete implementations.
package whatsapp

import "context"

// SendResult is returned after a message is accepted by the underlying provider.
type SendResult struct {
	MessageID string `json:"message_id"`
	Provider  string `json:"provider"`
}

// MediaKind classifies an outgoing media message.
type MediaKind string

const (
	MediaImage    MediaKind = "image"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaDocument MediaKind = "document"
	MediaSticker  MediaKind = "sticker" // WebP image sent as a sticker
)

// MediaMessage is an outgoing media payload (raw bytes + metadata).
type MediaMessage struct {
	Kind     MediaKind
	Data     []byte
	Mimetype string
	FileName string // used for documents
	Caption  string // used for image/video/document
}

// Provider is the common contract both workflows implement so the REST layer
// stays identical regardless of which one is active.
//
// Only SendText is implemented in the skeleton; media/document/sticker sends are
// the natural next step to develop manually on each provider.
type Provider interface {
	// Name identifies the active workflow, e.g. "whatsapp-api" or "whatsapp-business".
	Name() string

	// Connect establishes the session. For the WhatsMeow workflow this may block
	// until QR pairing completes on first run; for the Business workflow it just
	// validates configuration.
	Connect(ctx context.Context) error

	// Disconnect releases any resources / connections held by the provider.
	Disconnect()

	// IsReady reports whether the provider can currently send messages.
	IsReady() bool

	// QRCode returns the latest login QR string when the provider is waiting to be
	// paired, or an empty string when no pairing is needed / already logged in.
	QRCode() string

	// SendText sends a plain text message. `to` is a phone number in international
	// format without '+' (e.g. "628123456789").
	SendText(ctx context.Context, to, body string) (SendResult, error)

	// SendMedia uploads and sends a media message (photo, video, audio, document).
	SendMedia(ctx context.Context, to string, m MediaMessage) (SendResult, error)
}
