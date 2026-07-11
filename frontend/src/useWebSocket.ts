import { useEffect, useRef, useState } from "react";
import type { ConnStatus, Message } from "./types";


export function useWebSocket(onMessage: (m: Message) => void): ConnStatus {
  const [status, setStatus] = useState<ConnStatus>("connecting");

  const handlerRef = useRef(onMessage);
  handlerRef.current = onMessage;

  useEffect(() => {
    let ws: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let closed = false; // set on unmount so we stop reconnecting

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      setStatus("connecting");
      ws = new WebSocket(`${proto}://${location.host}/ws`);

      ws.onopen = () => setStatus("open");
      ws.onmessage = (ev) => {
        try {
          handlerRef.current(JSON.parse(ev.data) as Message);
        } catch {
          // ignore non-JSON frames
        }
      };
      ws.onclose = () => {
        setStatus("closed");
        if (!closed) retry = setTimeout(connect, 2000);
      };
      ws.onerror = () => ws?.close();
    };

    connect();

    return () => {
      closed = true;
      clearTimeout(retry);
      ws?.close();
    };
  }, []);

  return status;
}
