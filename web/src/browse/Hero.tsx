import { useRef, useState } from "react";
import { motion } from "framer-motion";
import { CaretLeft, CaretRight, Info, Play } from "@phosphor-icons/react";
import type { Ott, Slide } from "../catalog/types";
import { Img } from "../components/Img";
import { usePlay } from "./usePlay";

/** The featured carousel. Swiping is native scroll-snap (smooth on
 * low-end phones); the visible slide is reported up so the ambient
 * light behind the page follows it. */
export function Hero({
  ott,
  slides,
  onIndex,
  onDetails,
}: {
  ott: Ott;
  slides: Slide[];
  onIndex: (i: number) => void;
  onDetails: (id: string) => void;
}) {
  const play = usePlay();
  const scroller = useRef<HTMLDivElement>(null);
  const [index, setIndex] = useState(0);

  function onScroll() {
    const el = scroller.current;
    if (!el) return;
    const i = Math.round(el.scrollLeft / el.clientWidth);
    if (i !== index) {
      setIndex(i);
      onIndex(i);
    }
  }

  function goTo(i: number) {
    const el = scroller.current;
    if (el) el.scrollTo({ left: i * el.clientWidth, behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" });
  }

  if (slides.length === 0) return null;

  return (
    <section aria-label="Featured" className="catalog-hero">
      <div
        ref={scroller}
        onScroll={onScroll}
        className="flex overflow-x-auto no-scrollbar snap-x snap-mandatory"
      >
        {slides.map((s) => (
          <div key={s.id} className="hero-slide w-full shrink-0 snap-center px-4">
            <button
              onClick={() => onDetails(s.id)}
              aria-label={s.title ? `About ${s.title}` : "View featured title"}
              className="hero-art relative block w-full aspect-[4/3] overflow-hidden rounded-[28px] shimmer ring-1 ring-white/10"
            >
              <Img src={s.img} loading="eager" className="absolute inset-0 size-full object-cover" />
              <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/10 to-transparent" />
              {s.logo && (
                <Img
                  src={s.logo}
                  loading="eager"
                  className="absolute bottom-5 left-5 max-h-[72px] max-w-[68%] object-contain object-left-bottom drop-shadow-[0_2px_12px_rgba(0,0,0,0.6)]"
                />
              )}
              {!s.logo && s.title && <span className="absolute bottom-5 left-5 right-5 text-left text-3xl font-bold leading-tight text-bulb">{s.title}</span>}
              {s.rating && (
                <span className="absolute right-4 top-4 rounded-md border border-white/40 bg-black/30 px-1.5 py-0.5 text-[11px] font-semibold text-bulb backdrop-blur-md">
                  {s.rating}
                </span>
              )}
            </button>
            <p className="hero-description mt-3.5 px-1 text-[14px] leading-relaxed text-bulb/70 line-clamp-2">{s.desc}</p>
            <div className="hero-actions mt-4 flex gap-2.5 px-1">
              <motion.button
                whileTap={{ scale: 0.96 }}
                onClick={() => play(ott, s.id)}
                className="flex flex-1 items-center justify-center gap-2 rounded-2xl bg-spot py-3.5 text-[15px] font-bold text-spot-ink shadow-[0_8px_28px_-6px_rgba(249,115,22,0.55)]"
              >
                <Play weight="fill" className="size-4" />
                Play
              </motion.button>
              <motion.button
                whileTap={{ scale: 0.96 }}
                onClick={() => onDetails(s.id)}
                className="flex flex-1 items-center justify-center gap-2 rounded-2xl bg-white/10 py-3.5 text-[15px] font-semibold text-bulb ring-1 ring-white/10 backdrop-blur-md"
              >
                <Info weight="bold" className="size-4" />
                Details
              </motion.button>
            </div>
          </div>
        ))}
      </div>
      {slides.length > 1 && (
        <div className="mt-4 flex items-center justify-center gap-2">
          <button onClick={() => goTo(Math.max(0, index - 1))} disabled={index === 0} aria-label="Previous featured title" className="carousel-control"><CaretLeft weight="bold" /></button>
          {slides.map((s, i) => (
            <button key={s.id} onClick={() => goTo(i)} aria-label={`Featured title ${i + 1}`} aria-current={i === index ? "true" : undefined} className="grid h-9 w-6 place-items-center">
              <span className={`h-1 rounded-full ${i === index ? "w-5 bg-spot" : "w-1.5 bg-white/25"}`} />
            </button>
          ))}
          <button onClick={() => goTo(Math.min(slides.length - 1, index + 1))} disabled={index === slides.length - 1} aria-label="Next featured title" className="carousel-control"><CaretRight weight="bold" /></button>
        </div>
      )}
    </section>
  );
}
