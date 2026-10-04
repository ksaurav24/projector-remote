import { useBookmarks } from "./context";
import { BookmarkButton } from "./BookmarkButton";
import { PosterCard } from "../browse/Rail";
import { ottLabel, type Ott } from "../catalog/types";

export function Bookmarks({ onOpen }: { onOpen: (ott: Ott, id: string) => void }) {
  const { items, ready, error, refresh } = useBookmarks();
  return (
    <section className="px-4 pt-4" aria-label="Bookmarks">
      <h1 className="text-2xl font-bold">Bookmarks</h1>
      <p className="mt-2 text-sm text-haze">Your saved movies, shows, and anime, across all platforms.</p>
      {error && <div role="alert" className="mt-5 text-sm text-red-300">{error} <button onClick={refresh} className="font-semibold underline">Retry</button></div>}
      {!ready && !error && <p className="mt-8 text-haze" role="status">Loading bookmarks…</p>}
      {ready && !items.length && <p className="mt-10 text-haze">Open a title and tap Bookmark to save it here.</p>}
      <div className="mt-6 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 xl:grid-cols-4">
        {items.map(item => <article key={`${item.ott}:${item.titleId}`} className="min-w-0">
          <PosterCard ott={item.ott} item={{ id: item.titleId, poster: item.poster, title: item.title }} layout="h" onOpen={() => onOpen(item.ott, item.titleId)} className="w-full" />
          <h2 className="mt-2 text-sm font-semibold">{item.title}</h2>
          <p className="mb-3 mt-1 text-xs text-haze">{ottLabel(item.ott)}</p>
          <BookmarkButton item={item} />
        </article>)}
      </div>
    </section>
  );
}
