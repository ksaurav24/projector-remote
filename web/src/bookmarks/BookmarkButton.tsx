import { useState } from "react";
import { BookmarkSimple } from "@phosphor-icons/react";
import { useBookmarks, type Bookmark } from "./context";

export function BookmarkButton({ item }: { item: Bookmark }) {
  const { items, ready, setSaved, error: loadError, refresh } = useBookmarks();
  const saved = items.some(b => b.ott === item.ott && b.titleId === item.titleId);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function toggle() {
    if (busy) return;
    setBusy(true); setError("");
    try { await setSaved(item, !saved); }
    catch (err) { setError(err instanceof Error ? err.message : "Couldn't save bookmark. Try again."); }
    finally { setBusy(false); }
  }
  return (
    <div>
      <button onClick={toggle} disabled={busy || !ready} aria-pressed={saved} aria-label={`${saved ? "Remove bookmark for" : "Bookmark"} ${item.title}`} className="flex items-center gap-2 rounded-xl bg-white/10 px-4 py-2.5 text-sm font-semibold text-bulb ring-1 ring-white/15 disabled:opacity-50">
        <BookmarkSimple weight={saved ? "fill" : "bold"} className="size-4" />
        {busy ? "Saving…" : saved ? "Bookmarked" : "Bookmark"}
      </button>
      {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}
      {!ready && loadError && <button onClick={refresh} className="mt-2 text-sm text-spot">Retry loading bookmarks</button>}
    </div>
  );
}
