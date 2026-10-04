import type { Ott } from "../catalog/types";

export interface Track {
  id: string;
  lang: string;
  label: string;
}

export interface VideoTrack {
  id: string;
  height: number;
  bitrate: number;
}

export type Status = "loading" | "playing" | "paused" | "buffering" | "ended" | "error";

/** The projector's playback session, as the server broadcasts it
 * (internal/playback.NowPlaying). Always a full snapshot. */
export interface NowPlaying {
  ott: Ott;
  titleId: string;
  contentId: string;
 seasonId: string;
  title: string;
  epLabel: string;
  epTitle: string;
  poster: string;
  nextLabel: string;
  status: Status;
  pos: number;
  dur: number;
  tracks: { audio: Track[]; text: Track[]; video: VideoTrack[] };
  selected: { audio: string; text: string; video: string };
  countdown: { seconds: number; label: string } | null;
  error: string;
  /** The error is fixed by logging in to NetMirror (LoginCard). */
  needsLogin: boolean;
}
