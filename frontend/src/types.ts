// Message mirrors internal/websocket/models.go (the raw WS frame + history item).
export interface Message {
  id: string;
  chat_jid: string;
  sender_jid: string;
  kind: string; // text | image | video | audio | document | sticker
  body: string;
  media_path?: string;
  timestamp: string; // RFC3339
}

// SendResult mirrors whatsapp.SendResult (response of POST /messages/text).
export interface SendResult {
  message_id: string;
  provider: string;
}

export type ConnStatus = "connecting" | "open" | "closed";
