import { useRef, useState } from "react";
import { clock } from "../format";

/** A seek bar. Dragging previews the target time; the seek is sent
 * once, on release, so the projector isn't flooded with seeks. */
export function Scrubber({
  pos,
  dur,
  onSeek,
}: {
  pos: number;
  dur: number;
  onSeek: (ms: number) => void;
}) {
  const track = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const shown = drag ?? pos;
  const ratio = dur > 0 ? Math.min(1, Math.max(0, shown / dur)) : 0;

  function at(clientX: number) {
    const r = track.current!.getBoundingClientRect();
    return Math.min(1, Math.max(0, (clientX - r.left) / r.width)) * dur;
  }

  return (
    <div>
      <div
        ref={track}
        role="slider"
        aria-label="Playback position"
        aria-valuemin={0}
        aria-valuemax={Math.round(dur / 1000)}
        aria-valuenow={Math.round(shown / 1000)}
        aria-valuetext={clock(shown)}
        onPointerDown={(e) => {
          if (dur <= 0) return;
          (e.target as Element).setPointerCapture(e.pointerId);
          setDrag(at(e.clientX));
        }}
        onPointerMove={(e) => drag !== null && setDrag(at(e.clientX))}
        onPointerUp={() => {
          if (drag !== null) onSeek(Math.round(drag));
          setDrag(null);
        }}
        onPointerCancel={() => setDrag(null)}
        className="relative flex h-8 touch-none select-none items-center"
      >
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/15">
          <div className="h-full bg-spot" style={{ width: `${ratio * 100}%` }} />
        </div>
        <div
          className={`absolute top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-bulb shadow-[0_2px_10px_rgba(0,0,0,0.5)] transition-[width,height] duration-150 ${drag !== null ? "size-6" : "size-4"}`}
          style={{ left: `${ratio * 100}%` }}
        />
      </div>
      <div className="mt-1 flex justify-between text-[12px] tabular-nums text-haze">
        <span className={drag !== null ? "text-bulb" : ""}>{clock(shown)}</span>
        <span>{dur > 0 ? `-${clock(dur - shown)}` : "--:--"}</span>
      </div>
    </div>
  );
}
