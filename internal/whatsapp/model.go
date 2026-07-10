package whatsapp

import "go.mau.fi/whatsmeow"

type IncomingMedia struct {
	msg      whatsmeow.DownloadableMessage
	kind     MediaKind
	mimetype string
	filename string // documents only; empty otherwise
	caption  string
}
