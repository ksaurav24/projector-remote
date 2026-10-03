export type Ott = "nf" | "pv" | "hs";

export const OTTS: { id: Ott; label: string; tint: string }[] = [
  { id: "nf", label: "Netflix", tint: "#E50914" },
  { id: "pv", label: "Prime", tint: "#1A98FF" },
  { id: "hs", label: "Hotstar", tint: "#1F80E0" },
];

export function ottLabel(ott: Ott): string {
  return OTTS.find((o) => o.id === ott)!.label;
}

export interface Poster {
  id: string;
  poster: string;
}

export interface Slide {
  id: string;
  img: string;
  logo: string;
  desc: string;
  rating: string;
}

export interface Row {
  title: string;
  layout: "h" | "v";
  items: Poster[];
}

export interface Home {
  slider: Slide[];
  rows: Row[];
}

export interface SearchItem extends Poster {
  title: string;
}

export interface Season {
  id: string;
  number: number;
  label: string;
  episodes: number;
}

export interface Episode {
  id: string;
  num: number;
  title: string;
  desc: string;
  runtime: string;
  still: string;
}

export interface Position {
  pos: number;
  dur: number;
}

export interface EpisodePage {
  items: Episode[];
  nextPage: number | null;
  progress?: Record<string, Position>;
}

export interface Resume {
  episodeId: string;
  seasonId: string;
  epLabel: string;
  pos: number;
  dur: number;
}

export interface Title {
  id: string;
  type: "movie" | "series";
  title: string;
  year: string;
  rating: string;
  runtime: string;
  quality: string;
  desc: string;
  backdrop: string;
  poster: string;
  langs: { code: string; label: string }[];
  seasons: Season[];
  seasonId: string;
  episodes: EpisodePage | null;
  suggest: Poster[];
  resume: Resume | null;
  progress: Record<string, Position>;
}

export interface ContinueItem {
  ott: Ott;
  titleId: string;
  episodeId: string;
  title: string;
  epLabel: string;
  poster: string;
  pos: number;
  dur: number;
}

/** Card shape for a poster. Wide cards are NetMirror's 341x192 art;
 * tall ones differ per platform (Netflix 284x398, Hotstar 166x224).
 * Hotstar serves tall art everywhere, even for search and "more like
 * this". */
export function posterAspect(ott: Ott, layout: "h" | "v"): string {
  if (ott === "hs") return "3 / 4";
  return layout === "v" ? "5 / 7" : "16 / 9";
}
