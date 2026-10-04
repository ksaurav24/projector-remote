import { BookmarkButton } from "../bookmarks/BookmarkButton";
import { useCallback, useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import { CaretDown, Play, SpeakerHigh } from "@phosphor-icons/react";
import { catalogUrl, getJSON, useApi } from "../catalog/api";
import type { EpisodePage, Ott, Position, Title } from "../catalog/types";
import { Img } from "../components/Img";
import { clock, progressRatio, timeLeft } from "../format";
import { AmbientLight } from "./AmbientLight";
import { PosterCard } from "./Rail";
import { Sheet } from "./Sheet";
import { ErrorState } from "./States";
import { usePlay } from "./usePlay";

export function TitleSheet({
  ott,
  id,
  onClose,
  onOpenTitle,
}: {
  ott: Ott;
  id: string;
  onClose: () => void;
  onOpenTitle: (ott: Ott, id: string) => void;
}) {
  const t = useApi<Title>(catalogUrl("title", { ott, id }));
  return (
    <Sheet onClose={onClose}>
      {(startDrag) => (
        <>
          <AmbientLight src={t.data?.backdrop} className="absolute inset-x-0 top-0 h-[120vh]" />
          <div className="sticky top-0 z-20 h-0">
            <button
              onClick={onClose}
              aria-label="Close"
              className="absolute left-4 top-[calc(env(safe-area-inset-top)+12px)] grid size-10 place-items-center rounded-full bg-black/45 ring-1 ring-white/15 backdrop-blur-md"
            >
              <CaretDown weight="bold" className="size-5" />
            </button>
          </div>
          <div
            onPointerDown={startDrag}
            className="absolute inset-x-0 top-0 z-10 flex h-16 touch-none justify-center pt-[calc(env(safe-area-inset-top)+8px)]"
          >
            <span className="h-1.5 w-10 rounded-full bg-white/35" />
          </div>
          <div className="relative">
            {t.status === "loading" && <TitleSkeleton />}
            {t.status === "error" && (
              <div className="pt-24">
                <ErrorState message={t.error} onRetry={t.retry} />
              </div>
            )}
            {t.status === "ok" && <TitleBody key={`${ott}:${id}`} ott={ott} title={t.data} onOpenTitle={onOpenTitle} />}
          </div>
        </>
      )}
    </Sheet>
  );
}

function Badge({ children }: { children: string }) {
  return (
    <span className="rounded-[5px] border border-white/35 px-1.5 py-px text-[11px] font-semibold leading-4 text-bulb/90">
      {children}
    </span>
  );
}

function TitleBody({
  ott,
  title: t,
  onOpenTitle,
}: {
  ott: Ott;
  title: Title;
  onOpenTitle: (ott: Ott, id: string) => void;
}) {
  const play = usePlay();
  const [expanded, setExpanded] = useState(false);
  const series = t.type === "series";
  const [seasonId, setSeasonId] = useState(t.seasonId);
  const selected = t.seasons.find(s => s.id === seasonId || s.editions?.some(e => e.id === seasonId));
  const edition = selected?.editions?.find(e => e.id === seasonId);
  const r = t.resume && (!series || t.resume.seasonId === seasonId) ? t.resume : null;

  let sub = t.runtime;
  if (r && r.pos > 0) sub = series ? `${r.epLabel} at ${clock(r.pos)}` : `from ${clock(r.pos)}`;
  else if (r) sub = r.epLabel;
  else if (series) sub = ott === "anime" ? `${edition?.label ?? "Subtitled"} · Episode 1` : `S${selected?.number ?? 1} E1`;

  return (
    <>
      <div className="title-backdrop aspect-video w-full [mask-image:linear-gradient(to_bottom,black_55%,transparent)]">
        <Img src={t.backdrop} loading="eager" className="size-full object-cover" />
      </div>

      <div className="title-content relative -mt-8 px-5 pb-32">
        <h1 className="text-[34px] font-[760] leading-[1.02] tracking-[-0.025em] text-balance [font-stretch:86%]">
          {t.title}
        </h1>

        <div className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-[13px] text-haze">
          {t.year && <span>{t.year}</span>}
          {t.rating && <Badge>{t.rating}</Badge>}
          <span>{ott === "anime" ? `${selected?.episodes ?? 0} episodes · ${t.seasons.length} release${t.seasons.length === 1 ? "" : "s"}` : series ? `${t.seasons.length} season${t.seasons.length === 1 ? "" : "s"}` : t.runtime}</span>
          {t.quality && <Badge>{t.quality}</Badge>}
        </div>

        <motion.button
          whileTap={{ scale: 0.97 }}
          onClick={() => play(ott, t.id, series ? { seasonId, episodeId: r?.episodeId ?? "" } : undefined)}
          disabled={ott === "anime" && !selected?.episodes}
          className="disabled:opacity-40 disabled:cursor-not-allowed mt-6 flex w-full items-center justify-center gap-3 rounded-2xl bg-spot py-3.5 text-spot-ink shadow-[0_10px_32px_-8px_rgba(249,115,22,0.6)]"
        >
          <Play weight="fill" className="size-5" />
          <span className="text-[16px] font-bold">{r ? "Resume" : "Play"}</span>
          {sub && <span className="text-[14px] font-medium opacity-70">{sub}</span>}
        </motion.button>

        <div className="mt-3"><BookmarkButton item={{ ott, titleId: t.id, title: t.title, poster: t.poster }} /></div>

        {t.desc && (
          <button
            onClick={() => setExpanded((e) => !e)}
            className={`mt-5 text-left text-[15px] leading-relaxed text-bulb/80 ${expanded ? "" : "line-clamp-3"}`}
          >
            {t.desc}
          </button>
        )}

        {t.langs.length > 0 && (
          <p className="mt-4 flex gap-2 text-[13px] leading-snug text-haze">
            <SpeakerHigh weight="bold" className="mt-px size-4 shrink-0" aria-label="Audio" />
            {t.langs.map((l) => l.label).join(", ")}
          </p>
        )}

        {ott === "anime" && <p className="mt-4 text-sm text-haze">Choose subtitled or English dub below. Stream availability varies by episode.</p>}
        {series && t.episodes && <Episodes ott={ott} title={t} seasonId={seasonId} setSeasonId={setSeasonId} />}

        {t.suggest.length > 0 && (
          <section className="mt-10">
            <h2 className="mb-3 text-[17px] font-semibold tracking-[-0.01em]">More like this</h2>
            <div className={`grid gap-2.5 ${(ott === "hs" || ott === "anime") ? "grid-cols-3" : "grid-cols-2"}`}>
              {t.suggest.map((p) => (
                <PosterCard key={p.id} ott={ott} item={p} layout="h" onOpen={(sid) => onOpenTitle(ott, sid)} />
              ))}
            </div>
          </section>
        )}
      </div>
    </>
  );
}

/** A season's episodes, fetched a page (10) at a time as the list
 * scrolls into view. The title response carries the first page of its
 * opening season, so that one needs no request. The caller keys the
 * component using this by season, so switching seasons starts fresh. */
function useEpisodes(ott: Ott, seasonId: string, initial: EpisodePage | null) {
  const [pages, setPages] = useState<EpisodePage[]>(initial ? [initial] : []);
  const [loading, setLoading] = useState(initial === null);
  const [error, setError] = useState<string | null>(null);
  const hasInitial = initial !== null;

  const fetchPage = useCallback(
    (page: number, live: () => boolean) =>
      getJSON<EpisodePage>(catalogUrl("season", { ott, id: seasonId, page }))
        .then(
          (p) => {
            if (!live()) return;
            // Replace rather than append if this page is already in
            // (a repeated request must not duplicate episodes).
            setPages((ps) => [...ps.filter((q) => q.items[0]?.id !== p.items[0]?.id), p]);
          },
          () => live() && setError("Couldn't load more episodes."),
        )
        .finally(() => live() && setLoading(false)),
    [ott, seasonId],
  );

  useEffect(() => {
    if (hasInitial) return;
    let live = true;
    fetchPage(1, () => live);
    return () => {
      live = false;
    };
  }, [hasInitial, fetchPage]);

  const last = pages[pages.length - 1];
  const next = pages.length === 0 ? 1 : (last.nextPage ?? null);
  const loadPage = (page: number) => {
    setLoading(true);
    setError(null);
    fetchPage(page, () => true);
  };
  return {
    items: pages.flatMap((p) => p.items),
    progress: Object.assign({}, ...pages.map((p) => p.progress ?? {})) as Record<string, Position>,
    loading,
    error,
    hasMore: pages.length > 0 && next !== null,
    loadMore: () => {
      if (!loading && !error && next !== null) loadPage(next);
    },
    retry: () => loadPage(next ?? 1),
  };
}

function Episodes({ ott, title, seasonId, setSeasonId }: { ott: Ott; title: Title; seasonId: string; setSeasonId: (id: string) => void }) {
  const season = title.seasons.find((s) => s.id === seasonId || s.editions?.some(e => e.id === seasonId));
  const audio = season?.editions?.find(e => e.id === seasonId)?.code;
  const initial = seasonId === title.seasonId ? { ...title.episodes!, progress: title.progress } : null;

  return (
    <section className="mt-10">
      <h2 className="mb-3 text-[17px] font-semibold tracking-[-0.01em]">
        {title.seasons.length > 1 ? "Episodes" : (season?.label ?? "Episodes")}
      </h2>

      {title.seasons.length > 1 && (
        <div className="-mx-5 mb-5 flex gap-2 overflow-x-auto no-scrollbar px-5">
          {title.seasons.map((s) => (
            <button
              key={s.id}
              onClick={() => setSeasonId(s.editions?.find(e => e.code === audio)?.id ?? s.id)}
              className={
                "shrink-0 rounded-full px-4 py-2 text-[13px] font-semibold transition-colors " +
                (s.id === season?.id ? "bg-bulb text-black" : "bg-white/[0.08] text-bulb/80 ring-1 ring-white/10")
              }
            >
              {s.label}
            </button>
          ))}
        </div>
      )}

      {season?.editions && <div className="mb-5 flex flex-wrap items-center gap-2" role="group" aria-label="Audio">
        <span className="mr-2 text-sm text-haze">Audio</span>
        {season.editions.map(e => <button key={e.id} aria-pressed={e.id === seasonId} onClick={() => setSeasonId(e.id)} className={`rounded-full px-4 py-2 text-sm font-semibold ${e.id === seasonId ? "bg-bulb text-black" : "bg-white/10 text-bulb"}`}>{e.label}</button>)}
      </div>}
      <EpisodeList key={seasonId} ott={ott} titleId={title.id} seasonId={seasonId} initial={initial} />
    </section>
  );
}

function EpisodeList({
  ott,
  titleId,
  seasonId,
  initial,
}: {
  ott: Ott;
  titleId: string;
  seasonId: string;
  initial: EpisodePage | null;
}) {
  const play = usePlay();
  const eps = useEpisodes(ott, seasonId, initial);
  const sentinel = useRef<HTMLDivElement>(null);
  const { loadMore } = eps;

  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) loadMore();
    });
    io.observe(el);
    return () => io.disconnect();
  }, [loadMore]);

  return (
    <>
      {!eps.loading && !eps.error && eps.items.length === 0 && <p className="text-sm text-haze">No released episodes are listed yet.</p>}
      <ol className="flex flex-col gap-6">
        {eps.items.map((ep) => {
          const p = eps.progress[ep.id];
          return (
            <li key={ep.id}>
              <motion.button
                whileTap={{ scale: 0.98 }}
                onClick={() => play(ott, titleId, { seasonId, episodeId: ep.id })}
                className="flex w-full gap-3.5 text-left"
              >
                <div className="relative aspect-video w-[136px] shrink-0 overflow-hidden rounded-xl shimmer ring-1 ring-white/[0.08]">
                  <Img src={ep.still} className="absolute inset-0 size-full object-cover" />
                  <span className="absolute inset-0 grid place-items-center">
                    <span className="grid size-8 place-items-center rounded-full bg-black/45 ring-1 ring-white/25 backdrop-blur-sm">
                      <Play weight="fill" className="size-3.5" />
                    </span>
                  </span>
                  {p && (
                    <div className="absolute inset-x-0 bottom-0 h-[3px] bg-white/20">
                      <div className="h-full bg-spot" style={{ width: `${progressRatio(p.pos, p.dur) * 100}%` }} />
                    </div>
                  )}
                </div>
                <div className="min-w-0 flex-1 py-0.5">
                  <div className="text-[15px] font-semibold leading-snug">
                    <span className="mr-1.5 tabular-nums text-haze">{ep.num}</span>
                    {ep.title}
                  </div>
                  <div className="mt-1 text-[12px] tabular-nums text-haze">
                    {p && p.dur > 0 ? timeLeft(p.pos, p.dur) : ep.runtime}
                  </div>
                </div>
              </motion.button>
              {ep.desc && <p className="mt-2.5 text-[13px] leading-relaxed text-bulb/60 line-clamp-2">{ep.desc}</p>}
            </li>
          );
        })}
      </ol>

      {eps.loading && (
        <div className="mt-6 flex gap-3.5" aria-busy>
          <div className="aspect-video w-[136px] rounded-xl shimmer" />
          <div className="flex-1 pt-1">
            <div className="h-3.5 w-3/4 rounded-full shimmer" />
            <div className="mt-2 h-3 w-1/4 rounded-full shimmer" />
          </div>
        </div>
      )}
      {eps.error && (
        <button onClick={eps.retry} className="mt-5 text-[14px] font-semibold text-spot">
          {eps.error} Try again
        </button>
      )}
      {eps.hasMore && !eps.error && <div ref={sentinel} className="h-px" />}
    </>
  );
}

function TitleSkeleton() {
  return (
    <div aria-busy aria-label="Loading">
      <div className="aspect-video w-full shimmer" />
      <div className="px-5 pt-6">
        <div className="h-8 w-3/4 rounded-lg shimmer" />
        <div className="mt-4 h-3.5 w-1/2 rounded-full shimmer" />
        <div className="mt-6 h-[52px] rounded-2xl shimmer" />
        <div className="mt-6 h-3.5 w-full rounded-full shimmer" />
        <div className="mt-2 h-3.5 w-5/6 rounded-full shimmer" />
      </div>
    </div>
  );
}
