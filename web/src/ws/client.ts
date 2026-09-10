export type ServerMessage =
  | { type: "status"; connected: boolean }
  | { type: "apps"; items: string[] }
  | { type: "volume"; value: number };

export type ClientMessage =
  | { type: "move"; dx: number; dy: number }
  | { type: "pointerdown" }
  | { type: "pointerup" }
  | { type: "key"; code: string }
  | { type: "text"; value: string }
  | { type: "launch"; pkg: string }
  | { type: "refresh_apps" }
  | { type: "set_volume"; level: number };

type Listener = (msg: ServerMessage) => void;

export class WsClient {
  private ws: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private url: string;
  private attempt = 0;
  private closedByUser = false;

  constructor(url: string) {
    this.url = url;
    this.connect();
  }

  private connect() {
    const ws = new WebSocket(this.url);
    this.ws = ws;

    ws.onopen = () => {
      this.attempt = 0;
    };

    ws.onmessage = (ev) => {
      let msg: ServerMessage;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      for (const l of this.listeners) l(msg);
    };

    ws.onclose = () => {
      for (const l of this.listeners) l({ type: "status", connected: false });
      if (this.closedByUser) return;
      const delay = Math.min(1000 * 2 ** this.attempt, 10000);
      this.attempt += 1;
      setTimeout(() => this.connect(), delay);
    };

    ws.onerror = () => {
      ws.close();
    };
  }

  subscribe(l: Listener): () => void {
    this.listeners.add(l);
    return () => this.listeners.delete(l);
  }

  send(msg: ClientMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  close() {
    this.closedByUser = true;
    this.ws?.close();
  }
}
