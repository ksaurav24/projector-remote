import { createContext, useContext } from "react";
import type { Ott } from "../catalog/types";
export interface Bookmark { ott: Ott; titleId: string; title: string; poster: string }
export interface BookmarkState {
  items: Bookmark[];
  ready: boolean;
  error: string;
  refresh: () => void;
  setSaved: (item: Bookmark, saved: boolean) => Promise<void>;
}
export const BookmarkContext = createContext<BookmarkState | null>(null);
export function useBookmarks() {
  const value = useContext(BookmarkContext);
  if (!value) throw new Error("Missing BookmarkProvider");
  return value;
}
