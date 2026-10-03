import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { MagnifyingGlass } from "@phosphor-icons/react";
import { catalogUrl, useApi } from "../catalog/api";
import type { ContinueItem, Home, Ott } from "../catalog/types";
import { useOverlays } from "../nav/Overlays";
import { useWs } from "../ws/WsContext";
import { AmbientLight } from "./AmbientLight";
import { Hero } from "./Hero";
import { OttSwitcher } from "./OttSwitcher";
import { ContinueRow, Rail } from "./Rail";
import { ErrorState, HomeSkeleton } from "./States";

const OTT_KEY = "browse.ott";

function storedOtt(): Ott {
  try {
    const v = localStorage.getItem(OTT_KEY);
    if (v === "nf" || v === "pv" || v === "hs") return v;
  } catch {
    // Private mode or storage disabled: fall through to the default.
  }
  return "nf";
}

export function BrowsePage() {
  const { open } = useOverlays();
  const [ott, setOttState] = useState<Ott>(storedOtt);
  const [heroIndex, setHeroIndex] = useState(0);
  const home = useApi<Home>(catalogUrl("home", { ott }));
  const { historyEpoch, connected } = useWs();
  const cont = useApi<{ items: ContinueItem[] }>(`/api/catalog/continue?v=${historyEpoch}`);
  // Keep showing the last list while a refreshed one loads, rather than
  // letting the row blink out.
  const [contItems, setContItems] = useState<ContinueItem[]>([]);
  if (cont.data && cont.data.items !== contItems) setContItems(cont.data.items);

  function setOtt(o: Ott) {
    setOttState(o);
    setHeroIndex(0);
    try {
      localStorage.setItem(OTT_KEY, o);
    } catch {
      // Not remembering the choice is fine.
    }
  }

  const openTitle = (o: Ott, id: string) => open({ kind: "title", ott: o, id });
  const slide = home.data?.slider[heroIndex];

  return (
    <div className="relative min-h-full bg-black text-bulb">
      <AmbientLight src={slide?.img} className="fixed inset-x-0 top-0 h-[90vh]" />

      <header className="sticky top-0 z-20 flex items-center justify-between gap-3 bg-black/35 px-4 pb-3 pt-[calc(env(safe-area-inset-top)+12px)] backdrop-blur-xl">
        <OttSwitcher value={ott} onChange={setOtt} layoutGroup="browse" />
        {!connected && (
          <span className="ml-auto flex items-center gap-1.5 text-[12px] font-medium text-red-400">
            <span className="size-1.5 animate-pulse rounded-full bg-red-400" />
            Reconnecting
          </span>
        )}
        <motion.button
          whileTap={{ scale: 0.92 }}
          onClick={() => open({ kind: "search", ott })}
          aria-label="Search"
          className="grid size-10 place-items-center rounded-full bg-white/[0.08] ring-1 ring-white/10"
        >
          <MagnifyingGlass weight="bold" className="size-[18px]" />
        </motion.button>
      </header>

      <AnimatePresence mode="wait" initial={false}>
        <motion.main
          key={ott}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.2 }}
          className="relative pb-48 pt-3"
        >
          {home.status === "loading" && <HomeSkeleton />}
          {home.status === "error" && <ErrorState message={home.error} onRetry={home.retry} />}
          {home.status === "ok" && (
            <>
              <Hero
                ott={ott}
                slides={home.data.slider}
                onIndex={setHeroIndex}
                onDetails={(id) => openTitle(ott, id)}
              />
              <ContinueRow items={contItems} onOpen={openTitle} />
              {home.data.rows.map((row) => (
                <Rail key={row.title} ott={ott} row={row} onOpen={(id) => openTitle(ott, id)} />
              ))}
            </>
          )}
        </motion.main>
      </AnimatePresence>
    </div>
  );
}
