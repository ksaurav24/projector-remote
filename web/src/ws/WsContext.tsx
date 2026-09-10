import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { WsClient } from "./client";
import type { ServerMessage, ClientMessage } from "./client";

function wsUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${window.location.host}/ws`;
}

interface WsContextValue {
  connected: boolean;
  apps: string[];
  volume: number;
  send: (msg: ClientMessage) => void;
}

const WsContext = createContext<WsContextValue | null>(null);

export function WsProvider({ children }: { children: ReactNode }) {
  // The WsClient (and the WebSocket connection it opens) is a side
  // effect, so it's created inside useEffect, not useMemo - useMemo's
  // factory isn't guaranteed to run exactly once (React may invoke it
  // an extra time and discard the result, notably under StrictMode's
  // dev-mode double-invoke), which previously left the socket
  // permanently marked "closed by the user" after that discarded
  // instance's cleanup ran, even though a real connection had briefly
  // succeeded.
  const clientRef = useRef<WsClient | null>(null);
  const [connected, setConnected] = useState(false);
  const [apps, setApps] = useState<string[]>([]);
  const [volume, setVolume] = useState(0);

  useEffect(() => {
    const client = new WsClient(wsUrl());
    clientRef.current = client;
    const unsub = client.subscribe((msg: ServerMessage) => {
      if (msg.type === "status") setConnected(msg.connected);
      if (msg.type === "apps") setApps(msg.items);
      if (msg.type === "volume") setVolume(msg.value);
    });
    return () => {
      unsub();
      client.close();
      clientRef.current = null;
    };
  }, []);

  const value: WsContextValue = {
    connected,
    apps,
    volume,
    send: (msg: ClientMessage) => clientRef.current?.send(msg),
  };

  return <WsContext.Provider value={value}>{children}</WsContext.Provider>;
}

export function useWs(): WsContextValue {
  const ctx = useContext(WsContext);
  if (!ctx) throw new Error("useWs must be used within WsProvider");
  return ctx;
}
