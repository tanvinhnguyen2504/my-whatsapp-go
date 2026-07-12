import { useCallback, useEffect, useRef, useState } from "react";
import { sendText } from "./api";
import type { ConnStatus, Message, SendResult, ServerEvent } from "./types";

const ACK_TIMEOUT_MS = 4000; // wait this long for an ack per attempt
const RECONNECT_WINDOW_MS = 10000; // keep retrying over WS before falling back to REST
const POLL_MS = 300; // how often to re-check for a reopened socket

type Pending = {
  resolve: (r: SendResult) => void;
  reject: (e: SendFailure) => void;
  timer: ReturnType<typeof setTimeout>;
};

// SendFailure.permanent separates a server business error (ok:false — do NOT fall
// back, REST would fail identically) from a transport failure (retry / REST).
class SendFailure extends Error {
  permanent: boolean;
  constructor(message: string, permanent: boolean) {
    super(message);
    this.permanent = permanent;
  }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export function useWebSocket(onMessage: (m: Message) => void) {
  const [status, setStatus] = useState<ConnStatus>("connecting");

  // Keep the latest handler in a ref so the socket callbacks never go stale.
  const handlerRef = useRef(onMessage);
  handlerRef.current = onMessage;

  const wsRef = useRef<WebSocket | null>(null);
  const pendingRef = useRef<Map<string, Pending>>(new Map());

  useEffect(() => {
    let retry: ReturnType<typeof setTimeout> | undefined;
    let closed = false; // set on unmount so we stop reconnecting

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      setStatus("connecting");
      const ws = new WebSocket(`${proto}://${location.host}/ws`);
      wsRef.current = ws;

      ws.onopen = () => setStatus("open");
      ws.onmessage = (e) => {
        let ev: ServerEvent;
        try {
          ev = JSON.parse(e.data);
        } catch {
          return; // ignore non-JSON frames
        }
        if (ev.type === "message") {
          handlerRef.current(ev.data);
        } else if (ev.type === "ack") {
          const p = pendingRef.current.get(ev.req_id);
          if (!p) {
            return; // late/duplicate ack — already settled
          }
          clearTimeout(p.timer);
          pendingRef.current.delete(ev.req_id);
          if (ev.ok){
             p.resolve({ message_id: ev.message_id ?? "", provider: "ws" });
          }
          else {
             p.reject(new SendFailure(ev.error ?? "send failed", true));
          }
        }
      };
      ws.onclose = () => {
        setStatus("closed");
        if (!closed) retry = setTimeout(connect, 2000);
      };
      ws.onerror = () => ws.close();
    };

    connect();
    return () => {
      closed = true;
      clearTimeout(retry);
      wsRef.current?.close();
    };
  }, []);

  // awaitAck registers a pending entry that the onmessage handler settles, or
  // rejects (transport) if no ack arrives in time.
  const awaitAck = useCallback((reqId: string): Promise<SendResult> => {
    return new Promise<SendResult>((resolve, reject) => {
      const timer = setTimeout(() => {
        pendingRef.current.delete(reqId);
        reject(new SendFailure("ack timeout", false));
      }, ACK_TIMEOUT_MS);
      pendingRef.current.set(reqId, { resolve, reject, timer });
    });
  }, []);

  // send delivers a message with a staged fallback: WebSocket first — retried
  // across reconnects with the SAME req_id (the server dedupes, so no double
  // send) — then REST as a last resort. A server business error is thrown
  // immediately without falling back, since REST would return the same error.
  const send = useCallback(
    async (to: string, body: string): Promise<SendResult> => {
      const reqId = crypto.randomUUID();
      const deadline = Date.now() + RECONNECT_WINDOW_MS;

      while (Date.now() < deadline) {
        const ws = wsRef.current;
        if (ws && ws.readyState === WebSocket.OPEN) {
          console.log('send by webhook...');
          ws.send(JSON.stringify({ type: "send", req_id: reqId, to, body }));
          try {
            return await awaitAck(reqId);
          } catch (e) {
            if (e instanceof SendFailure && e.permanent) throw e; // business error
            // transport timeout: loop and retry (the socket may have died)
          }
        } else {
          await sleep(POLL_MS); // socket not open — wait for the auto-reconnect
        }
      }

      // WebSocket exhausted (transport failure) → REST fallback.
      // Note: REST carries no idempotency key, so a message the server actually
      // sent but whose ack was lost could send twice; add a key to /messages/text
      // to fully close that window.
      return sendText(to, body);
    },
    [awaitAck],
  );

  return { status, send };
}
