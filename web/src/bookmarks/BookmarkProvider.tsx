import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { BookmarkContext, type Bookmark } from "./context";

export function BookmarkProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Bookmark[]>([]);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const mutations = useRef(0);
  const refresh = useCallback(async () => {
    if (mutations.current) return;
    const version = ++generation.current;
    try {
      const response = await fetch("/api/catalog/bookmarks", { cache: "no-store" });
      if (!response.ok) throw new Error("Couldn't load bookmarks. Try again.");
      const body = await response.json();
      if (generation.current !== version) return;
      setItems(body.items); setReady(true); setError("");
    } catch {
      if (generation.current === version) setError("Couldn't load bookmarks. Check your connection and retry.");
    }
  }, []);
  const invalidate = useCallback(() => { ++generation.current; }, []);
  useEffect(() => {
    const initial = window.setTimeout(() => { void refresh(); }, 0);
    const onFocus = () => { void refresh(); };
    const interval = window.setInterval(() => { if (!document.hidden) void refresh(); }, 30000);
    window.addEventListener("focus", onFocus);
    return () => { invalidate(); clearTimeout(initial); clearInterval(interval); window.removeEventListener("focus", onFocus); };
  }, [refresh, invalidate]);
  const setSaved = useCallback(async (item: Bookmark, saved: boolean) => {
    mutations.current++; ++generation.current;
    try {
      const query = new URLSearchParams({ ott: item.ott, id: item.titleId });
      const response = await fetch(`/api/catalog/bookmarks?${query}`, {
        method: saved ? "PUT" : "DELETE",
        headers: { "Content-Type": "application/json" },
        body: saved ? JSON.stringify({ title: item.title, poster: item.poster }) : undefined,
      });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error || "Couldn't update bookmark. Try again.");
      // Apply only this mutation; another title may be saving concurrently.
      setItems(previous => {
        const others = previous.filter(b => b.ott !== item.ott || b.titleId !== item.titleId);
        return saved ? [item, ...others] : others;
      });
      setReady(true); setError("");
    } finally {
      mutations.current--;
      if (!mutations.current) void refresh();
    }
  }, [refresh]);
  return <BookmarkContext.Provider value={{ items, ready, error, refresh, setSaved }}>{children}</BookmarkContext.Provider>;
}
