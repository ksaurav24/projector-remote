import { useEffect, useState } from "react";
import type { NowPlaying } from "./types";

/** The playback position, moving smoothly between the server's
 * once-a-second snapshots: while playing, it advances locally from the
 * last reported position. */
export function useLivePos(np: NowPlaying | null): number {
  // Keyed by the snapshot it's counting from, so a new report (or a
  // pause) restarts the count instead of adding to a stale one.
  const key = np ? `${np.contentId}:${np.pos}:${np.status}` : "";
  const playing = np?.status === "playing";
  const [tick, setTick] = useState({ key: "", elapsed: 0 });

  useEffect(() => {
    if (!playing) return;
    const start = performance.now();
    const id = window.setInterval(() => setTick({ key, elapsed: performance.now() - start }), 250);
    return () => window.clearInterval(id);
  }, [key, playing]);

  if (!np) return 0;
  const live = np.pos + (playing && tick.key === key ? tick.elapsed : 0);
  return np.dur > 0 ? Math.min(np.dur, live) : live;
}
