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

// ClientCommand is a frame the browser sends over the socket.
export interface ClientCommand {
  type: "send";
  req_id: string;
  to: string;
  body: string;
}

// ServerEvent is a frame the server sends: "message" carries an inbound
// broadcast; "ack" carries the result of a send command, keyed by req_id.
export type ServerEvent =
  | { type: "message"; data: Message }
  | { type: "ack"; req_id: string; ok: boolean; message_id?: string; error?: string };

// SendState is the client-side lifecycle of an outgoing message bubble.
export type SendState = "sending" | "sent" | "failed";
