import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { WsClient } from "./client";
import type { ServerMessage, ClientMessage } from "./client";
import type { ProjectorSettings, ProjectorSettingsPatch } from "./client";
import type { NowPlaying } from "../playback/types";
import { invalidate } from "../catalog/api";

function wsUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${window.location.host}/ws`;
}

interface WsContextValue {
  connected: boolean;
  apps: string[];
  volume: number;
  projectorSettings: ProjectorSettings | null;
  settingsError: string | null;
  nowPlaying: NowPlaying | null;
  /** Bumps whenever watch history may have changed (a session moved to
   * a new stream or ended), so views showing progress can refetch. */
  historyEpoch: number;
  send: (msg: ClientMessage) => void;
}

const WsContext = createContext<WsContextValue | null>(null);

function mergeSettings(current: ProjectorSettings, patch: ProjectorSettingsPatch): ProjectorSettings {
  return {
    ...current,
    ...patch,
    features: patch.features ? { ...current.features, ...patch.features } : current.features,
    corners: patch.corners ? { ...current.corners, ...patch.corners } : current.corners,
  };
}

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
  const [projectorSettings, setProjectorSettings] = useState<ProjectorSettings | null>(null);
  const [settingsError, setSettingsError] = useState<string | null>(null);
  const [nowPlaying, setNowPlaying] = useState<NowPlaying | null>(null);
  const [historyEpoch, setHistoryEpoch] = useState(0);
  const lastContent = useRef<string | null>(null);
  const settingsRevision = useRef(-1);
  const confirmedSettings = useRef<ProjectorSettings | null>(null);
  const pendingSettings = useRef<Record<string, unknown>>({});

  useEffect(() => {
    const client = new WsClient(wsUrl());
    clientRef.current = client;
    const unsub = client.subscribe((msg: ServerMessage) => {
      if (msg.type === "status") setConnected(msg.connected);
      if (msg.type === "apps") setApps(msg.items);
      if (msg.type === "volume") setVolume(msg.value);
      if (msg.type === "projector_settings") {
        if (msg.revision < settingsRevision.current) return;
        settingsRevision.current = msg.revision;
        confirmedSettings.current = msg.settings;
        setProjectorSettings((current) => {
          if (!current) return msg.settings;
          const next = {
            ...msg.settings,
            features: { ...msg.settings.features },
            corners: { ...msg.settings.corners },
          };
          for (const [key, value] of Object.entries(pendingSettings.current)) {
            if (key.startsWith("features.")) {
              next.features[key.slice("features.".length)] = Boolean(value);
            } else if (key.startsWith("corners.")) {
              const [, corner, axis] = key.split(".");
              const point = next.corners[corner as keyof typeof next.corners];
              next.corners[corner as keyof typeof next.corners] = { ...point, [axis]: Number(value) };
            } else {
              (next as unknown as Record<string, unknown>)[key] = value;
            }
          }
          return next;
        });
        setSettingsError(null);
      }
      if (msg.type === "projector_settings_patch") {
        if (msg.revision <= settingsRevision.current) return;
        settingsRevision.current = msg.revision;
        if (confirmedSettings.current) confirmedSettings.current = mergeSettings(confirmedSettings.current, msg.patch);

        const visiblePatch: ProjectorSettingsPatch = { ...msg.patch };
        for (const key of ["zoom", "horizontal", "vertical", "mode"] as const) {
          if (!(key in visiblePatch)) continue;
          if (!(key in pendingSettings.current)) continue;
          if (pendingSettings.current[key] === visiblePatch[key]) delete pendingSettings.current[key];
          else delete visiblePatch[key];
        }
        if (visiblePatch.features) {
          visiblePatch.features = { ...visiblePatch.features };
          for (const [key, value] of Object.entries(visiblePatch.features)) {
            const pendingKey = `features.${key}`;
            if (!(pendingKey in pendingSettings.current)) continue;
            if (pendingSettings.current[pendingKey] === value) delete pendingSettings.current[pendingKey];
            else delete visiblePatch.features[key];
          }
        }
        if (visiblePatch.corners) {
          visiblePatch.corners = { ...visiblePatch.corners };
          for (const [corner, point] of Object.entries(visiblePatch.corners)) {
            for (const axis of ["x", "y"] as const) {
              const pendingKey = `corners.${corner}.${axis}`;
              if (!(pendingKey in pendingSettings.current)) continue;
              if (pendingSettings.current[pendingKey] === point[axis]) delete pendingSettings.current[pendingKey];
              else delete visiblePatch.corners[corner as keyof typeof visiblePatch.corners];
            }
          }
        }
        setProjectorSettings((current) => current ? mergeSettings(current, visiblePatch) : current);
        setSettingsError(null);
      }
      if (msg.type === "settings_error") {
        pendingSettings.current = {};
        if (confirmedSettings.current) setProjectorSettings(confirmedSettings.current);
        setSettingsError(msg.message);
      }
      if (msg.type === "now_playing") {
        setNowPlaying(msg.session);
        const content = msg.session?.contentId || null;
        if (content !== lastContent.current) {
          lastContent.current = content;
          // Positions and continue-watching are saved server-side as
          // sessions change; drop cached copies so they're refetched.
          invalidate("/api/catalog/continue");
          invalidate("/api/catalog/title");
          invalidate("/api/catalog/season");
          setHistoryEpoch((n) => n + 1);
        }
      }
    });
    return () => {
      unsub();
      client.close();
      clientRef.current = null;
    };
  }, []);

  function send(msg: ClientMessage) {
    let patch: ProjectorSettingsPatch | null = null;
    if (msg.type === "set_zoom") {
      patch = { zoom: msg.percent };
      pendingSettings.current.zoom = msg.percent;
    } else if (msg.type === "set_keystone") {
      patch = { [msg.axis]: msg.angle };
      pendingSettings.current[msg.axis] = msg.angle;
    } else if (msg.type === "set_projection_mode") {
      patch = { mode: msg.mode };
      pendingSettings.current.mode = msg.mode;
    } else if (msg.type === "set_projection_feature") {
      patch = { features: { [msg.key]: msg.enabled } };
      pendingSettings.current[`features.${msg.key}`] = msg.enabled;
    } else if (msg.type === "set_corner") {
      patch = { corners: { [msg.corner]: { x: msg.x, y: msg.y } } };
      pendingSettings.current[`corners.${msg.corner}.x`] = msg.x;
      pendingSettings.current[`corners.${msg.corner}.y`] = msg.y;
    }
    if (patch) setProjectorSettings((current) => current ? mergeSettings(current, patch) : current);
    clientRef.current?.send(msg);
  }

  const value: WsContextValue = {
    connected,
    apps,
    volume,
    projectorSettings,
    settingsError,
    nowPlaying,
    historyEpoch,
    send,
  };

  return <WsContext.Provider value={value}>{children}</WsContext.Provider>;
}

export function useWs(): WsContextValue {
  const ctx = useContext(WsContext);
  if (!ctx) throw new Error("useWs must be used within WsProvider");
  return ctx;
}
