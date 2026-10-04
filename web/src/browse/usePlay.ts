import { useOverlays } from "../nav/Overlays";
import { useCallback } from "react";
import { useWs } from "../ws/WsContext";
import type { Ott } from "../catalog/types";
import { tick } from "../format";

/** Starts a title on the projector. Without an episode, a series picks
 * up where it left off (or at S1 E1): the server decides. */
export function usePlay() {
  const { send } = useWs();
 const { open } = useOverlays();
  return useCallback(
    (ott: Ott, titleId: string, episode?: { seasonId: string; episodeId: string }) => {
      tick();
      send({ type: "play", ott, titleId, ...episode });
 open({ kind: "nowplaying" });
    },
    [send, open],
  );
}
