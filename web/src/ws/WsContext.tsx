import { createContext, useContext, useEffect, useMemo, useState } from "react";
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
  send: (msg: ClientMessage) => void;
}

const WsContext = createContext<WsContextValue | null>(null);

export function WsProvider({ children }: { children: ReactNode }) {
  const client = useMemo(() => new WsClient(wsUrl()), []);
  const [connected, setConnected] = useState(false);
  const [apps, setApps] = useState<string[]>([]);

  useEffect(() => {
    const unsub = client.subscribe((msg: ServerMessage) => {
      if (msg.type === "status") setConnected(msg.connected);
      if (msg.type === "apps") setApps(msg.items);
    });
    return () => {
      unsub();
      client.close();
    };
  }, [client]);

  const value: WsContextValue = {
    connected,
    apps,
    send: (msg: ClientMessage) => client.send(msg),
  };

  return <WsContext.Provider value={value}>{children}</WsContext.Provider>;
}

export function useWs(): WsContextValue {
  const ctx = useContext(WsContext);
  if (!ctx) throw new Error("useWs must be used within WsProvider");
  return ctx;
}
