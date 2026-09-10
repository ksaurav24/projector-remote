import netflixIcon from "../assets/app-icons/netflix.svg";
import youtubeIcon from "../assets/app-icons/youtube.svg";
import spotifyIcon from "../assets/app-icons/spotify.svg";
import disneyPlusIcon from "../assets/app-icons/disneyplus.svg";
import primeVideoIcon from "../assets/app-icons/primevideo.svg";
import netmirrorIcon from "../assets/app-icons/netmirror.png";

export interface CuratedApp {
  pkg: string;
  label: string;
  icon: string;
  /**
   * "glyph": a square brand mark meant to sit on a solid `tint`
   * tile (Netflix, YouTube, Spotify) - matches how these apps'
   * real icons look.
   * "wordmark": a wide brand logo (Disney+, Prime Video) that
   * needs to sit on a neutral tile, letter-boxed, not stretched
   * to a square.
   * "raster": already a complete app icon image with its own
   * background baked in (NetMirror's real icon, extracted from
   * its installed APK) - render as-is.
   */
  kind: "glyph" | "wordmark" | "raster";
  tint?: string;
}

/** Pinned above the general app grid, in this exact order. */
export const FAVORITE_PKGS = ["app.netmirror.newtv", "com.google.android.youtube.tv"];

export const CURATED_APPS: CuratedApp[] = [
  { pkg: "app.netmirror.newtv", label: "NetMirror", icon: netmirrorIcon, kind: "raster" },
  { pkg: "com.google.android.youtube.tv", label: "YouTube", icon: youtubeIcon, kind: "glyph", tint: "#FF0000" },
  { pkg: "com.netflix.ninja", label: "Netflix", icon: netflixIcon, kind: "glyph", tint: "#E50914" },
  { pkg: "com.amazon.avod.thirdpartyclient", label: "Prime Video", icon: primeVideoIcon, kind: "wordmark" },
  { pkg: "com.disney.disneyplus", label: "Disney+", icon: disneyPlusIcon, kind: "wordmark" },
  { pkg: "com.spotify.tv.android", label: "Spotify", icon: spotifyIcon, kind: "glyph", tint: "#1ED760" },
];
