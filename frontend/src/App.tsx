import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getHistory } from "./api";
import { useWebSocket } from "./useWebSocket";
import type { Message, SendState } from "./types";

// ChatMessage is a Message plus a client-only delivery state for outgoing bubbles.
type ChatMessage = Message & { state?: SendState };

function toJID(to: string): string {
  const digits = to.replace(/\D/g, "");
  return digits ? `${digits}@s.whatsapp.net` : "";
}

export default function App() {
  const [to, setTo] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");

  const chatJID = useMemo(() => toJID(to), [to]);

  const onMessage = useCallback(
    (m: Message) => {
      if (chatJID && m.chat_jid !== chatJID) {
        return;
      }
      setMessages((prev) => (prev.some((p) => p.id === m.id) ? prev : [...prev, m]));
    },
    [chatJID],
  );

  const { status, send } = useWebSocket(onMessage);

  // Load history whenever the recipient changes.
  useEffect(() => {
    setMessages([]);
    setError("");
    if (!chatJID) {
      return;
    }
    let cancelled = false;

    getHistory(chatJID)
      .then((hist) => {
        if (!cancelled) {
          setMessages(hist.slice().reverse()); // server returns newest-first
        }
      })
      .catch((e) => !cancelled && setError(String(e.message ?? e)));
    return () => {
      cancelled = true;
    };
  }, [chatJID]);

  // Autoscroll to the latest message.
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    listRef.current?.scrollTo(0, listRef.current.scrollHeight);
  }, [messages]);

  const handleSend = async () => {
    const body = draft.trim();
    if (!body || !to.trim()) {
      return;
    }

    // Optimistic local echo, marked "sending" until the send settles.
    const tempId = crypto.randomUUID();
    const optimistic: ChatMessage = {
      id: tempId,
      chat_jid: chatJID,
      sender_jid: "me",
      kind: "text",
      body,
      timestamp: new Date().toISOString(),
      state: "sending",
    };
    setMessages((prev) => [...prev, optimistic]);
    setDraft("");
    setError("");

    try {
      const res = await send(to.trim(), body);
      setMessages((prev) =>
        prev.map((m) =>
          m.id === tempId ? { ...m, id: res.message_id || tempId, state: "sent" } : m,
        ),
      );
    } catch (e) {
      setMessages((prev) =>
        prev.map((m) => (m.id === tempId ? { ...m, state: "failed" } : m)),
      );
      setError(String((e as Error).message ?? e));
    }
  };

  return (
    <div className="app">
      <header className="topbar">
        <h1>my-whatsapp</h1>
        <span className={`status status-${status}`}>{status}</span>
      </header>

      <div className="recipient">
        <label htmlFor="to">To</label>
        <input
          id="to"
          placeholder="recipient number, e.g. 84900000000"
          value={to}
          onChange={(e) => setTo(e.target.value)}
        />
      </div>

      <div className="messages" ref={listRef}>
        {!chatJID && <p className="hint">Enter a recipient number to start.</p>}
        {chatJID && messages.length === 0 && <p className="hint">No messages yet.</p>}
        {messages.map((m) => (
          <div key={m.id} className={`bubble ${m.sender_jid === "me" ? "out" : "in"}`}>
            {m.kind !== "text" && <span className="kind">[{m.kind}]</span>}
            <span className="body">{m.body || m.media_path}</span>
            <span className="meta">
              <time>{new Date(m.timestamp).toLocaleTimeString()}</time>
              {m.sender_jid === "me" && m.state && (
                <span className={`tick ${m.state}`}>
                  {m.state === "sending" ? "…" : m.state === "sent" ? "✓" : "!"}
                </span>
              )}
            </span>
          </div>
        ))}
      </div>

      {error && <div className="error">{error}</div>}

      <div className="composer">
        <input
          placeholder="Type a message"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleSend()}
          disabled={!to.trim()}
        />
        <button onClick={handleSend} disabled={!draft.trim() || !to.trim()}>
          Send
        </button>
      </div>
    </div>
  );
}
