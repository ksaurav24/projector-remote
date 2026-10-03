import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { CaretLeft, MagnifyingGlass, X } from "@phosphor-icons/react";
import { catalogUrl, useApi } from "../catalog/api";
import { ottLabel, posterAspect } from "../catalog/types";
import type { Ott, SearchItem } from "../catalog/types";
import { Img } from "../components/Img";
import { OttSwitcher } from "./OttSwitcher";
import { ErrorState } from "./States";

const DEBOUNCE_MS = 300;

export function SearchSheet({
  ott: initialOtt,
  onClose,
  onOpenTitle,
}: {
  ott: Ott;
  onClose: () => void;
  onOpenTitle: (ott: Ott, id: string) => void;
}) {
  const [ott, setOtt] = useState(initialOtt);
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");

  useEffect(() => {
    const t = window.setTimeout(() => setQuery(text.trim()), DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [text]);

  const res = useApi<{ items: SearchItem[] }>(query ? catalogUrl("search", { ott, q: query }) : null);

  return (
    <motion.div
      role="dialog"
      aria-modal
      initial={{ opacity: 0, y: 24 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 24 }}
      transition={{ type: "spring", stiffness: 420, damping: 38 }}
      className="fixed inset-0 z-40 overflow-y-auto overscroll-contain bg-black text-bulb"
    >
      <header className="sticky top-0 z-10 bg-black/70 px-4 pb-3 pt-[calc(env(safe-area-inset-top)+12px)] backdrop-blur-xl">
        <div className="flex items-center gap-2">
          <button onClick={onClose} aria-label="Back" className="-ml-1 grid size-10 shrink-0 place-items-center rounded-full">
            <CaretLeft weight="bold" className="size-5" />
          </button>
          <label className="flex flex-1 items-center gap-2.5 rounded-2xl bg-white/[0.08] px-3.5 ring-1 ring-white/10 focus-within:ring-spot/70">
            <MagnifyingGlass weight="bold" className="size-4 shrink-0 text-haze" />
            <input
              autoFocus
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={`Search ${ottLabel(ott)}`}
              enterKeyHint="search"
              className="min-w-0 flex-1 bg-transparent py-3 text-[16px] outline-none placeholder:text-haze"
            />
            {text && (
              <button onClick={() => setText("")} aria-label="Clear" className="text-haze">
                <X weight="bold" className="size-4" />
              </button>
            )}
          </label>
        </div>
        <div className="mt-3 flex">
          <OttSwitcher value={ott} onChange={setOtt} layoutGroup="search" />
        </div>
      </header>

      <div className="px-4 pb-16 pt-2">
        {!query && (
          <p className="pt-16 text-center text-[15px] text-haze">Find a movie or show on {ottLabel(ott)}.</p>
        )}
        {query && res.status === "loading" && <ResultsSkeleton ott={ott} />}
        {query && res.status === "error" && <ErrorState message={res.error} onRetry={res.retry} />}
        {query && res.status === "ok" && res.data.items.length === 0 && (
          <div className="pt-16 text-center">
            <p className="text-[15px] text-bulb/85">Nothing on {ottLabel(ott)} matches “{query}”.</p>
            <p className="mt-1.5 text-[14px] text-haze">Check the spelling, or search another platform.</p>
          </div>
        )}
        {query && res.status === "ok" && res.data.items.length > 0 && (
          <div className={`grid gap-x-2.5 gap-y-4 ${ott === "hs" ? "grid-cols-3" : "grid-cols-2"}`}>
            {res.data.items.map((it) => (
              <motion.button
                key={it.id}
                whileTap={{ scale: 0.96 }}
                onClick={() => onOpenTitle(ott, it.id)}
                className="text-left"
              >
                <div
                  style={{ aspectRatio: posterAspect(ott, "h") }}
                  className="relative overflow-hidden rounded-xl shimmer ring-1 ring-white/[0.08]"
                >
                  <Img src={it.poster} className="absolute inset-0 size-full object-cover" />
                </div>
                <div className="mt-1.5 text-[13px] font-medium leading-snug text-bulb/85 line-clamp-2">{it.title}</div>
              </motion.button>
            ))}
          </div>
        )}
      </div>
    </motion.div>
  );
}

function ResultsSkeleton({ ott }: { ott: Ott }) {
  return (
    <div aria-busy className={`grid gap-x-2.5 gap-y-4 ${ott === "hs" ? "grid-cols-3" : "grid-cols-2"}`}>
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i}>
          <div style={{ aspectRatio: posterAspect(ott, "h") }} className="rounded-xl shimmer" />
          <div className="mt-2 h-3 w-3/4 rounded-full shimmer" />
        </div>
      ))}
    </div>
  );
}
