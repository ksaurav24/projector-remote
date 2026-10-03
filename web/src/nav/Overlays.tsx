import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import type { Ott } from "../catalog/types";

export type Overlay =
  | { kind: "title"; ott: Ott; id: string }
  | { kind: "search"; ott: Ott }
  | { kind: "nowplaying" };

interface OverlayContextValue {
  stack: Overlay[];
  open: (o: Overlay) => void;
  close: () => void;
}

const OverlayContext = createContext<OverlayContextValue | null>(null);

/** A stack of full-screen overlays (title detail, search, now
 * playing) mirrored into browser history, so the phone's back gesture
 * closes the top overlay instead of leaving the PWA. Each open pushes
 * a history entry carrying the new stack depth; popstate trims the
 * stack back to whatever depth the restored entry carries. */
export function OverlayProvider({ children }: { children: ReactNode }) {
  const [stack, setStack] = useState<Overlay[]>([]);
  // Tracked outside state so pushState runs exactly once per open
  // (a state updater can be invoked twice under StrictMode).
  const depth = useRef(0);

  useEffect(() => {
    const onPop = (e: PopStateEvent) => {
      const d = typeof e.state?.overlayDepth === "number" ? e.state.overlayDepth : 0;
      depth.current = d;
      setStack((s) => s.slice(0, d));
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const open = useCallback((o: Overlay) => {
    depth.current += 1;
    window.history.pushState({ overlayDepth: depth.current }, "");
    setStack((s) => [...s, o]);
  }, []);

  const close = useCallback(() => window.history.back(), []);

  return <OverlayContext.Provider value={{ stack, open, close }}>{children}</OverlayContext.Provider>;
}

export function useOverlays(): OverlayContextValue {
  const ctx = useContext(OverlayContext);
  if (!ctx) throw new Error("useOverlays must be used within OverlayProvider");
  return ctx;
}
