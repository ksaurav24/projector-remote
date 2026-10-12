import type { NowPlaying, AnimeSkipSettings, SkipMode } from "../playback/types";

export interface ProjectorSettings {
  zoom: number;
  minZoom: number;
  horizontal: number;
  vertical: number;
  mode: "front" | "rear" | "ceiling-front" | "ceiling-rear";
  features: Record<string, boolean>;
  corners: Record<Corner, CornerPoint>;
  cornerMaxX: number;
  cornerMaxY: number;
}

export type Corner = "lt" | "rt" | "rb" | "lb";

export interface CornerPoint {
  x: number;
  y: number;
}

export type ProjectorSettingsPatch = Partial<Omit<ProjectorSettings, "features" | "corners">> & {
  features?: Record<string, boolean>;
  corners?: Partial<Record<Corner, CornerPoint>>;
};

export type ServerMessage =
  | { type: "anime_skip_settings"; settings: AnimeSkipSettings }
  | { type: "anime_skip_error"; message: string }
  | { type: "status"; connected: boolean }
  | { type: "apps"; items: string[] }
  | { type: "volume"; value: number }
  | { type: "projector_settings"; settings: ProjectorSettings; revision: number }
  | { type: "projector_settings_patch"; patch: ProjectorSettingsPatch; revision: number }
  | { type: "settings_error"; message: string }
  | { type: "now_playing"; session: NowPlaying | null };

export type ClientMessage =
  | { type: "set_anime_skip"; kind: "intro" | "outro"; mode: SkipMode }
  | { type: "skip_segment"; contentId: string; loadId: string; kind: "intro" | "outro" }
  | { type: "move"; dx: number; dy: number }
  | { type: "pointerdown" }
  | { type: "pointerup" }
  | { type: "key"; code: string }
  | { type: "text"; value: string }
  | { type: "launch"; pkg: string }
  | { type: "refresh_apps" }
  | { type: "set_volume"; level: number }
  | { type: "set_zoom"; percent: number }
  | { type: "set_keystone"; axis: "horizontal" | "vertical"; angle: number }
  | { type: "nudge_corner"; corner: "lt" | "rt" | "rb" | "lb"; direction: "up" | "down" | "left" | "right" }
  | { type: "set_corner"; corner: Corner; x: number; y: number; final?: boolean }
  | { type: "reset_keystone" }
  | { type: "set_projection_mode"; mode: ProjectorSettings["mode"] }
  | { type: "set_projection_feature"; key: string; enabled: boolean }
  | { type: "focus_start"; direction: "near" | "far" }
  | { type: "focus_stop" }
  | { type: "play"; ott: string; titleId: string; seasonId?: string; episodeId?: string }
  | { type: "set_paused"; paused: boolean }
  | { type: "seek"; to: number }
  | { type: "skip"; by: number }
  | { type: "select_track"; kind: "audio" | "text" | "video"; id: string }
  | { type: "next" }
  | { type: "stop" }
  | { type: "cancel_autoplay" }
  | { type: "autoplay_now" };

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
