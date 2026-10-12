import { useSyncExternalStore } from "react";

// Which optional catalogues are reachable. The laptop library ("local")
// comes and goes with the laptop's media server, so it's polled while the
// page is visible and rechecked whenever the page comes back into view.

// known is false until the projector has answered once.
type Sources = { local: boolean; known: boolean };

let current: Sources = { local: false, known: false };
const listeners = new Set<() => void>();
let timer: number | undefined;

async function refresh() {
  try {
    const res = await fetch("/api/catalog/sources", { cache: "no-store" });
    if (!res.ok) return;
    const next = { local: Boolean((await res.json()).local), known: true };
    if (next.local !== current.local || !current.known) {
      current = next;
      listeners.forEach((l) => l());
    }
  } catch {
    // Offline or the projector restarting: keep the last answer.
  }
}

function onVisible() {
  if (document.visibilityState === "visible") void refresh();
}

function subscribe(l: () => void) {
  listeners.add(l);
  if (listeners.size === 1) {
    void refresh();
    timer = window.setInterval(() => document.visibilityState === "visible" && void refresh(), 30_000);
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
  }
  return () => {
    listeners.delete(l);
    if (listeners.size === 0) {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", onVisible);
    }
  };
}

export function useSources(): Sources {
  return useSyncExternalStore(subscribe, () => current);
}
