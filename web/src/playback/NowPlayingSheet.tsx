import { useDialogFocus } from "../hooks/useDialogFocus";
import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  ArrowClockwise,
  ArrowCounterClockwise,
  CaretDown,
  Check,
  FilmSlate,
  Pause,
  Play,
  SkipForward,
  SpeakerHigh,
  Stop,
  Subtitles,
} from "@phosphor-icons/react";
import { posterAspect } from "../catalog/types";
import { Img } from "../components/Img";
import { tick } from "../format";
import { AmbientLight } from "../browse/AmbientLight";
import { Sheet } from "../browse/Sheet";
import { usePlay } from "../browse/usePlay";
import { useWs } from "../ws/WsContext";
import { LoginCard } from "./LoginCard";
import { Ring } from "./Ring";
import { Scrubber } from "./Scrubber";
import type { NowPlaying } from "./types";
import { useLivePos } from "./useLivePos";

type Kind = "audio" | "text" | "video";
interface Option {
  id: string;
  label: string;
}

function options(np: NowPlaying, kind: Kind): Option[] {
  if (kind === "audio") return np.tracks.audio.map((t) => ({ id: t.id, label: t.label }));
  if (kind === "text") return [{ id: "off", label: "Off" }, ...np.tracks.text.map((t) => ({ id: t.id, label: t.label }))];
  const videos = [...np.tracks.video].sort((a, b) => b.height - a.height || b.bitrate - a.bitrate);
  return [
    { id: "auto", label: "Auto" },
    ...videos.map((v) => {
      // Two variants can share a height (1280x720 and 940x720); the
      // bitrate tells them apart.
      const twin = videos.filter((o) => o.height === v.height).length > 1;
      return { id: v.id, label: `${v.height}p${twin ? `, ${(v.bitrate / 1e6).toFixed(1)} Mbps` : ""}` };
    }),
  ];
}

function selectedLabel(np: NowPlaying, kind: Kind): string {
  const id = np.selected[kind];
  if (kind === "video" && (id === "auto" || !id)) return "Auto";
  if (kind === "text" && (id === "off" || !id)) return "Off";
  return options(np, kind).find((o) => o.id === id)?.label ?? "Default";
}

const KINDS: { kind: Kind; name: string; Icon: typeof SpeakerHigh }[] = [
  { kind: "audio", name: "Audio", Icon: SpeakerHigh },
  { kind: "text", name: "Subtitles", Icon: Subtitles },
  { kind: "video", name: "Quality", Icon: FilmSlate },
];

export function NowPlayingSheet({ onClose }: { onClose: () => void }) {
  const { nowPlaying: np } = useWs();
  const observed = useRef(false);
 const ended = np === null;

  // Playback ended elsewhere (Back on the projector, another phone):
  // nothing left to control.
  useEffect(() => {
    if (!ended) observed.current = true;
 else if (observed.current) onClose();
  }, [ended, onClose]);

  if (!np) return <Sheet onClose={onClose} compact>{() => <div className="p-8"><button onClick={onClose} className="mb-6 rounded-full bg-white/10 px-4 py-2">Minimize player</button><p role="status">Starting on the projector…</p></div>}</Sheet>;
  return <SheetBody np={np} onClose={onClose} />;
}

function SheetBody({ np, onClose }: { np: NowPlaying; onClose: () => void }) {
  const { send } = useWs();
  const play = usePlay();
  const pos = useLivePos(np);
  const [picker, setPicker] = useState<Kind | null>(null);
  const tall = np.ott === "hs" || np.ott === "anime";
  const busy = np.status === "loading" || np.status === "buffering";

  const cmd = (m: Parameters<typeof send>[0]) => {
    tick();
    send(m);
  };

  return (
    <Sheet onClose={onClose} compact>
      {(startDrag) => (
        <>
          <AmbientLight src={np.poster} className="absolute inset-x-0 top-0 h-[110vh]" />
          <div
            onPointerDown={startDrag}
            className="absolute inset-x-0 top-0 z-10 flex h-16 touch-none justify-center pt-[calc(env(safe-area-inset-top)+8px)]"
          >
            <span className="h-1.5 w-10 rounded-full bg-white/35" />
          </div>
          <button
            onClick={onClose}
            aria-label="Minimize player"
            className="absolute left-4 top-[calc(env(safe-area-inset-top)+12px)] z-20 grid size-10 place-items-center rounded-full bg-black/40 ring-1 ring-white/15 backdrop-blur-md"
          >
            <CaretDown weight="bold" className="size-5" />
          </button>

          <div className="relative mx-auto flex min-h-full max-w-md flex-col px-6 pb-[calc(env(safe-area-inset-bottom)+28px)] pt-[calc(env(safe-area-inset-top)+72px)]">
            <div
              style={{ aspectRatio: posterAspect(np.ott, "h") }}
              className={`relative mx-auto overflow-hidden rounded-[26px] bg-white/[0.06] shadow-[0_24px_60px_-12px_rgba(0,0,0,0.7)] ring-1 ring-white/10 ${tall ? "w-3/5" : "w-full"}`}
            >
              {np.poster && <Img src={np.poster} loading="eager" className="absolute inset-0 size-full object-cover" />}
            </div>

            <h1 className="mt-7 text-[28px] font-[760] leading-[1.05] tracking-[-0.025em] text-balance [font-stretch:88%]">
              {np.title || "Starting…"}
            </h1>
            <p className="mt-1.5 flex items-center gap-2 text-[14px] text-haze">
              {np.epLabel && <span className="font-semibold text-bulb/80">{np.epLabel}</span>}
              {np.epTitle && <span className="truncate">{np.epTitle}</span>}
              {busy && (
                <span className="ml-auto flex items-center gap-1.5 text-[12px]">
                  <span className="size-1.5 animate-pulse rounded-full bg-spot" />
                  {np.status === "loading" ? "Starting" : "Buffering"}
                </span>
              )}
            </p>

            {np.status === "error" && np.needsLogin ? (
              <LoginCard onLoggedIn={() => play(np.ott, np.titleId, np.seasonId ? { seasonId: np.seasonId, episodeId: np.contentId } : undefined)} onDismiss={() => cmd({ type: "stop" })} />
            ) : np.status === "error" ? (
              <div className="mt-7 rounded-3xl bg-white/[0.07] p-5 ring-1 ring-white/10">
                <p className="text-[16px] font-semibold">Playback stopped</p>
                <p className="mt-1 text-[14px] leading-relaxed text-bulb/75">{np.error}</p>
                <div className="mt-4 flex gap-2.5">
                  <button
                    onClick={() => play(np.ott, np.titleId, np.seasonId ? { seasonId: np.seasonId, episodeId: np.contentId } : undefined)}
                    className="flex-1 rounded-2xl bg-spot py-3 text-[15px] font-bold text-spot-ink"
                  >
                    Try again
                  </button>
                  <button onClick={() => cmd({ type: "stop" })} className="flex-1 rounded-2xl bg-white/10 py-3 text-[15px] font-semibold ring-1 ring-white/10">
                    Dismiss
                  </button>
                </div>
              </div>
            ) : (
              <>
                <div className="mt-6">
                  <Scrubber pos={pos} dur={np.dur} onSeek={(to) => cmd({ type: "seek", to })} />
                </div>

                <div className="mt-3 flex items-center justify-center gap-10">
                  <TransportButton label="Back 10 seconds" onClick={() => cmd({ type: "skip", by: -10000 })}>
                    <ArrowCounterClockwise weight="bold" className="size-7" />
                    <span className="absolute text-[9px] font-bold">10</span>
                  </TransportButton>
                  <motion.button
                    whileTap={{ scale: 0.92 }}
                    onClick={() => cmd({ type: "set_paused", paused: np.status === "playing" })}
                    aria-label={np.status === "playing" ? "Pause" : "Play"}
                    className="grid size-20 place-items-center rounded-full bg-bulb text-black shadow-[0_10px_30px_-6px_rgba(245,239,232,0.35)]"
                  >
                    {np.status === "playing" ? <Pause weight="fill" className="size-8" /> : <Play weight="fill" className="size-8" />}
                  </motion.button>
                  <TransportButton label="Forward 10 seconds" onClick={() => cmd({ type: "skip", by: 10000 })}>
                    <ArrowClockwise weight="bold" className="size-7" />
                    <span className="absolute text-[9px] font-bold">10</span>
                  </TransportButton>
                </div>

                <AnimatePresence initial={false} mode="popLayout">
                  {np.countdown ? (
                    <motion.div
                      key="countdown"
                      initial={{ opacity: 0, y: 8 }}
                      animate={{ opacity: 1, y: 0 }}
                      exit={{ opacity: 0, y: 8 }}
                      className="mt-7 rounded-3xl bg-white/[0.07] p-4 ring-1 ring-white/10"
                    >
                      <div className="flex items-center gap-4">
                        <Ring seconds={np.countdown.seconds} size={52} />
                        <div>
                          <div className="text-[13px] text-haze">Up next</div>
                          <div className="text-[18px] font-bold">{np.countdown.label}</div>
                        </div>
                      </div>
                      <div className="mt-4 flex gap-2.5">
                        <button onClick={() => cmd({ type: "autoplay_now" })} className="flex-1 rounded-2xl bg-spot py-3 text-[15px] font-bold text-spot-ink">
                          Play now
                        </button>
                        <button onClick={() => cmd({ type: "cancel_autoplay" })} className="flex-1 rounded-2xl bg-white/10 py-3 text-[15px] font-semibold ring-1 ring-white/10">
                          Stay here
                        </button>
                      </div>
                    </motion.div>
                  ) : np.nextLabel ? (
                    <motion.button
                      key="next"
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      exit={{ opacity: 0 }}
                      whileTap={{ scale: 0.97 }}
                      onClick={() => cmd({ type: "next" })}
                      className="mt-7 flex w-full items-center justify-between rounded-2xl bg-white/10 px-5 py-4 ring-1 ring-white/10"
                    >
                      <span className="text-[15px] font-semibold">Next episode</span>
                      <span className="flex items-center gap-2 text-[14px] text-haze">
                        {np.nextLabel}
                        <SkipForward weight="fill" className="size-5 text-spot" />
                      </span>
                    </motion.button>
                  ) : null}
                </AnimatePresence>

                <div className="mt-4 grid grid-cols-3 gap-2.5">
                  {KINDS.map(({ kind, name, Icon }) => (
                    <motion.button
                      key={kind}
                      whileTap={{ scale: 0.96 }}
                      onClick={() => setPicker(kind)}
                      disabled={options(np, kind).length <= 1}
                      className="flex flex-col items-start gap-1.5 rounded-2xl bg-white/[0.07] p-3 text-left ring-1 ring-white/10 disabled:opacity-40"
                    >
                      <Icon weight="bold" className="size-[18px] text-haze" />
                      <span className="text-[12px] text-haze">{name}</span>
                      <span className="w-full truncate text-[14px] font-semibold">{selectedLabel(np, kind)}</span>
                    </motion.button>
                  ))}
                </div>
              </>
            )}

            <button
              onClick={() => cmd({ type: "stop" })}
              className="mx-auto mt-auto flex items-center gap-2 pt-8 text-[14px] font-semibold text-haze active:text-bulb"
            >
              <Stop weight="fill" className="size-4" />
              Stop playback
            </button>
          </div>

          <TrackPicker
            np={np}
            kind={picker}
            onPick={(kind, id) => {
              cmd({ type: "select_track", kind, id });
              setPicker(null);
            }}
            onClose={() => setPicker(null)}
          />
        </>
      )}
    </Sheet>
  );
}

function TransportButton({ label, onClick, children }: { label: string; onClick: () => void; children: ReactNode }) {
  return (
    <motion.button
      whileTap={{ scale: 0.88 }}
      onClick={onClick}
      aria-label={label}
      className="relative grid size-14 place-items-center rounded-full text-bulb active:bg-white/10"
    >
      {children}
    </motion.button>
  );
}

function TrackPicker({
  np,
  kind,
  onPick,
  onClose,
}: {
  np: NowPlaying;
  kind: Kind | null;
  onPick: (kind: Kind, id: string) => void;
  onClose: () => void;
}) {
  const panel = useDialogFocus(onClose, kind !== null);
  const name = KINDS.find((k) => k.kind === kind)?.name;
  return (
    <AnimatePresence>
      {kind && (
        <>
          <motion.div
            key="scrim"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="fixed inset-0 z-50 bg-black/60"
          />
          <motion.div
            key="panel"
            ref={panel}
            tabIndex={-1}
            aria-modal
            role="dialog"
            aria-label={name}
            initial={{ y: "100%" }}
            animate={{ y: 0 }}
            exit={{ y: "100%" }}
            transition={{ type: "spring", stiffness: 420, damping: 40 }}
            className="track-picker fixed inset-x-0 bottom-0 z-50 max-h-[70vh] overflow-y-auto rounded-t-[28px] bg-neutral-900 px-5 pb-[calc(env(safe-area-inset-bottom)+20px)] pt-3 ring-1 ring-white/10"
          >
            <div className="mx-auto mb-4 h-1.5 w-10 rounded-full bg-white/25" />
            <h2 className="mb-2 text-[17px] font-semibold">{name}</h2>
            <ul>
              {options(np, kind).map((o) => {
                const active = np.selected[kind] === o.id || (!np.selected[kind] && (o.id === "auto" || o.id === "off"));
                return (
                  <li key={o.id}>
                    <button
                      onClick={() => onPick(kind, o.id)}
                      className="flex w-full items-center justify-between border-b border-white/[0.06] py-3.5 text-left text-[16px]"
                    >
                      <span className={active ? "font-semibold text-bulb" : "text-bulb/80"}>{o.label}</span>
                      {active && <Check weight="bold" className="size-5 text-spot" />}
                    </button>
                  </li>
                );
              })}
            </ul>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
