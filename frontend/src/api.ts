import type { Message, SendResult } from "./types";

async function parseError(res: Response): Promise<string> {
  try {
    const data = await res.json();
    return data.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}

// sendText posts to the Go server's REST endpoint. The WebSocket is receive-only,
// so sending a message goes over HTTP, not the socket.
export async function sendText(to: string, body: string): Promise<SendResult> {
  const res = await fetch("/messages/text", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ to, body }),
  });
  if (!res.ok) throw new Error(await parseError(res));
  return res.json();
}

// getHistory loads the last stored messages for a chat JID.
export async function getHistory(chat: string): Promise<Message[]> {
  const res = await fetch(`/ws/history/${encodeURIComponent(chat)}`);
  if (!res.ok) throw new Error(await parseError(res));
  return (await res.json()) ?? [];
}
