import { useEffect, useRef, useState } from "react";
import { SpeakerHigh, SpeakerLow, SpeakerX } from "@phosphor-icons/react";
import { useWs } from "../ws/WsContext";

const TRACK_HEIGHT = 224;

function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n));
}

/** A physical volume-rocker-styled vertical slider. Drag sets an
 * absolute level directly (not simulated button repeats) via the
 * set_volume message, throttled to one send per animation frame
 * while dragging. Reflects the server-pushed volume (see WsContext)
 * whenever the user isn't actively dragging, so it stays in sync if
 * volume changes from elsewhere. */
export function VolumeSlider() {
  const { volume, send } = useWs();
  const trackRef = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);
  const rafId = useRef<number | null>(null);
  const pendingLevel = useRef<number | null>(null);
  const [displayLevel, setDisplayLevel] = useState(volume);

  useEffect(() => {
    if (!dragging.current) setDisplayLevel(volume);
  }, [volume]);

  function flush() {
    rafId.current = null;
    if (pendingLevel.current !== null) {
      send({ type: "set_volume", level: pendingLevel.current });
      pendingLevel.current = null;
    }
  }

  function schedule(level: number) {
    pendingLevel.current = level;
    if (rafId.current === null) {
      rafId.current = requestAnimationFrame(flush);
    }
  }

  function levelFromPointer(clientY: number): number {
    const rect = trackRef.current!.getBoundingClientRect();
    const ratio = 1 - (clientY - rect.top) / rect.height;
    return Math.round(clamp(ratio, 0, 1) * 100);
  }

  function onPointerDown(e: React.PointerEvent) {
    (e.target as Element).setPointerCapture(e.pointerId);
    dragging.current = true;
    const level = levelFromPointer(e.clientY);
    setDisplayLevel(level);
    schedule(level);
  }

  function onPointerMove(e: React.PointerEvent) {
    if (!dragging.current) return;
    const level = levelFromPointer(e.clientY);
    setDisplayLevel(level);
    schedule(level);
  }

  function onPointerUp() {
    dragging.current = false;
  }

  const Icon = displayLevel === 0 ? SpeakerX : displayLevel < 50 ? SpeakerLow : SpeakerHigh;

  return (
    <div className="flex flex-col items-center gap-2">
      <div
        ref={trackRef}
        role="slider"
        tabIndex={0}
        aria-orientation="vertical"
        aria-valuetext={`${displayLevel}%`}
        onKeyDown={(event) => {
          const delta = event.key === "ArrowUp" || event.key === "ArrowRight" ? 5 : event.key === "ArrowDown" || event.key === "ArrowLeft" ? -5 : 0;
          if (!delta && event.key !== "Home" && event.key !== "End") return;
          event.preventDefault();
          const next = event.key === "Home" ? 0 : event.key === "End" ? 100 : clamp(displayLevel + delta, 0, 100);
          setDisplayLevel(next);
          send({ type: "set_volume", level: next });
        }}
        aria-label="Volume"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={displayLevel}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        style={{ height: TRACK_HEIGHT }}
        className="relative w-12 rounded-full bg-neutral-100 dark:bg-neutral-800 border border-neutral-200 dark:border-neutral-700 touch-none select-none overflow-hidden"
      >
        <div
          className="absolute bottom-0 left-0 right-0 bg-orange-500 rounded-full transition-[height] duration-75"
          style={{ height: `${displayLevel}%` }}
        />
        <div
          className="absolute left-1/2 -translate-x-1/2 size-7 rounded-full bg-white dark:bg-neutral-100 border border-neutral-300 shadow-md"
          style={{ bottom: `calc(${displayLevel}% - 14px)` }}
        />
      </div>
      <span className="text-xs tabular-nums text-neutral-500 dark:text-neutral-400">{displayLevel}%</span>
      <Icon aria-hidden="true" weight="bold" className="size-5 text-neutral-500 dark:text-neutral-400" />
    </div>
  );
}
