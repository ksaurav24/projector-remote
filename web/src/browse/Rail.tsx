import { motion } from "framer-motion";
import { CaretLeft, CaretRight, Info, Play } from "@phosphor-icons/react";
import { useRef, type ReactNode } from "react";
import { posterAspect, tallPosters } from "../catalog/types";
import type { ContinueItem, Ott, Poster, Row } from "../catalog/types";
import { Img } from "../components/Img";
import { progressRatio, timeLeft } from "../format";
import { usePlay } from "./usePlay";

export function PosterCard({
  ott,
  item,
  layout,
  onOpen,
  className = "",
}: {
  ott: Ott;
  item: Poster;
  layout: "h" | "v";
  onOpen: (id: string) => void;
  className?: string;
}) {
  return (
    <motion.button
      whileTap={{ scale: 0.96 }}
      onClick={() => onOpen(item.id)}
      aria-label={item.title || "View title"}
      style={{ aspectRatio: posterAspect(ott, layout) }}
      className={`relative shrink-0 snap-start overflow-hidden rounded-2xl shimmer ring-1 ring-white/[0.08] ${className}`}
    >
      <Img src={item.poster} className="absolute inset-0 size-full object-cover" />
      {item.title && <span className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black via-black/75 to-transparent px-2 pb-2 pt-8 text-left text-xs font-semibold text-bulb">{item.title}</span>}
    </motion.button>
  );
}

function RailFrame({ title, children }: { title: string; children: ReactNode }) {
  const scroller = useRef<HTMLDivElement>(null);
  function scroll(direction: number) {
    const el = scroller.current;
    if (el) el.scrollBy({ left: direction * el.clientWidth * 0.8, behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" });
  }
  return (
    <section className="catalog-rail mt-8" aria-label={title}>
      <div className="mb-3 flex items-center justify-between gap-3 px-4">
        <h2 className="text-[17px] font-semibold tracking-[-0.01em] text-bulb">{title}</h2>
        <div className="hidden gap-2 lg:flex">
          <button onClick={() => scroll(-1)} aria-label={`Scroll ${title} left`} className="carousel-control"><CaretLeft weight="bold" /></button>
          <button onClick={() => scroll(1)} aria-label={`Scroll ${title} right`} className="carousel-control"><CaretRight weight="bold" /></button>
        </div>
      </div>
      <div ref={scroller} className="flex gap-3 overflow-x-auto no-scrollbar snap-x snap-mandatory scroll-px-4 px-4 pb-1">
        {children}
      </div>
    </section>
  );
}

function widthFor(ott: Ott, layout: "h" | "v") {
  return tallPosters(ott) || layout === "v" ? "w-[31vw] max-w-[150px]" : "w-[44vw] max-w-[220px]";
}

export function Rail({ ott, row, onOpen }: { ott: Ott; row: Row; onOpen: (id: string) => void }) {
  const ranked = /^top 10/i.test(row.title);
  return (
    <RailFrame title={row.title}>
      {row.items.map((p, i) =>
        ranked ? (
          <div key={p.id} className="flex shrink-0 snap-start items-end pl-1">
            <span aria-hidden className="rank-numeral relative z-0 -mr-4 select-none text-[152px]">
              {i + 1}
            </span>
            <PosterCard ott={ott} item={p} layout="v" onOpen={onOpen} className="relative z-10 w-[27vw] max-w-[128px]" />
          </div>
        ) : (
          <PosterCard key={p.id} ott={ott} item={p} layout={row.layout} onOpen={onOpen} className={widthFor(ott, row.layout)} />
        ),
      )}
    </RailFrame>
  );
}

/** Titles you've started, across all platforms. Tapping resumes on the
 * projector straight away; the info button opens the title instead. */
export function ContinueRow({
  items,
  onOpen,
}: {
  items: ContinueItem[];
  onOpen: (ott: Ott, id: string) => void;
}) {
  const play = usePlay();
  if (items.length === 0) return null;
  return (
    <RailFrame title="Continue watching">
      {items.map((c) => (
        <div
          key={`${c.ott}:${c.titleId}`}
          className={`shrink-0 snap-start ${tallPosters(c.ott) ? "w-[36vw] max-w-[170px]" : "w-[62vw] max-w-[280px]"}`}
        >
          <div className="relative">
            <motion.button
              whileTap={{ scale: 0.97 }}
              onClick={() => play(c.ott, c.titleId)}
              aria-label={`Resume ${c.title}`}
              style={{ aspectRatio: posterAspect(c.ott, "h") }}
              className="relative block w-full overflow-hidden rounded-2xl shimmer ring-1 ring-white/[0.08]"
            >
              <Img src={c.poster} className="absolute inset-0 size-full object-cover" />
              <div className="absolute inset-0 bg-gradient-to-t from-black/70 to-transparent" />
              <span className="absolute bottom-3 left-3 grid size-9 place-items-center rounded-full bg-black/45 ring-1 ring-white/25 backdrop-blur-md">
                <Play weight="fill" className="size-4 text-bulb" />
              </span>
              <div className="absolute inset-x-0 bottom-0 h-1 bg-white/15">
                <div className="h-full bg-spot" style={{ width: `${progressRatio(c.pos, c.dur) * 100}%` }} />
              </div>
            </motion.button>
            <button
              onClick={() => onOpen(c.ott, c.titleId)}
              aria-label={`About ${c.title}`}
              className="absolute right-2 top-2 grid size-8 place-items-center rounded-full bg-black/40 ring-1 ring-white/20 backdrop-blur-md"
            >
              <Info weight="bold" className="size-4 text-bulb" />
            </button>
          </div>
          <div className="mt-2 flex items-baseline justify-between gap-2 px-0.5">
            <span className="truncate text-[14px] font-semibold text-bulb">{c.title}</span>
            {c.dur > 0 && <span className="shrink-0 text-[12px] tabular-nums text-haze">{timeLeft(c.pos, c.dur)}</span>}
          </div>
          {c.epLabel && <div className="px-0.5 text-[12px] text-haze">{c.epLabel}</div>}
        </div>
      ))}
    </RailFrame>
  );
}
