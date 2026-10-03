import { motion } from "framer-motion";
import { CircleNotch, Pause, Play, X } from "@phosphor-icons/react";
import { posterAspect } from "../catalog/types";
import { Img } from "../components/Img";
import { clock, progressRatio, tick } from "../format";
import { useWs } from "../ws/WsContext";
import type { NowPlaying } from "./types";
import { useLivePos } from "./useLivePos";

/** Floating now-playing bar above the tab bar, on both tabs. Tapping it
 * opens the full controls. */
export function MiniBar({ np, onExpand }: { np: NowPlaying; onExpand: () => void }) {
  const { send } = useWs();
  const pos = useLivePos(np);
  const busy = np.status === "loading" || np.status === "buffering";

  let sub = np.epLabel ? `${np.epLabel}${np.epTitle ? `  ${np.epTitle}` : ""}` : `${clock(pos)} / ${clock(np.dur)}`;
  if (np.status === "loading") sub = "Starting on the projector";
  if (np.countdown) sub = `Up next: ${np.countdown.label} in ${np.countdown.seconds}s`;
  if (np.status === "error") sub = np.error;

  return (
    <motion.div
      initial={{ y: 24, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      exit={{ y: 24, opacity: 0 }}
      transition={{ type: "spring", stiffness: 420, damping: 36 }}
      className="fixed inset-x-3 bottom-[calc(env(safe-area-inset-bottom)+84px)] z-30 mx-auto max-w-md"
    >
      <div className="relative overflow-hidden rounded-[22px] bg-neutral-900/80 text-bulb shadow-[0_16px_40px_-10px_rgba(0,0,0,0.6)] ring-1 ring-white/10 backdrop-blur-xl">
        <button onClick={onExpand} className="flex w-full items-center gap-3 p-2.5 pr-28 text-left">
          <div
            style={{ aspectRatio: posterAspect(np.ott, "h") }}
            className="relative h-11 shrink-0 overflow-hidden rounded-xl bg-white/[0.08]"
          >
            {np.poster && <Img src={np.poster} className="absolute inset-0 size-full object-cover" />}
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-[14px] font-semibold">{np.title || "Starting…"}</div>
            <div className={`truncate text-[12px] ${np.status === "error" ? "text-red-300" : "text-haze"}`}>{sub}</div>
          </div>
        </button>

        <div className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-1">
          {np.countdown ? (
            <>
              <button
                onClick={() => {
                  tick();
                  send({ type: "autoplay_now" });
                }}
                className="rounded-full bg-spot px-3.5 py-2 text-[13px] font-bold text-spot-ink"
              >
                Play now
              </button>
              <button onClick={() => send({ type: "cancel_autoplay" })} aria-label="Cancel autoplay" className="grid size-10 place-items-center">
                <X weight="bold" className="size-4" />
              </button>
            </>
          ) : np.status === "error" ? (
            <button onClick={() => send({ type: "stop" })} aria-label="Dismiss" className="grid size-10 place-items-center">
              <X weight="bold" className="size-5" />
            </button>
          ) : busy ? (
            <span className="grid size-11 place-items-center" aria-label="Loading">
              <CircleNotch weight="bold" className="size-5 animate-spin text-haze" />
            </span>
          ) : (
            <button
              onClick={() => {
                tick();
                send({ type: "set_paused", paused: np.status === "playing" });
              }}
              aria-label={np.status === "playing" ? "Pause" : "Play"}
              className="grid size-11 place-items-center rounded-full bg-white/10"
            >
              {np.status === "playing" ? <Pause weight="fill" className="size-5" /> : <Play weight="fill" className="size-5" />}
            </button>
          )}
        </div>

        <div className="absolute inset-x-0 bottom-0 h-[2px] bg-white/10">
          <div className="h-full bg-spot" style={{ width: `${progressRatio(pos, np.dur) * 100}%` }} />
        </div>
      </div>
    </motion.div>
  );
}
