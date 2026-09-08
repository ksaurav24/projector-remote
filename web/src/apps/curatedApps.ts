export interface CuratedApp {
  pkg: string;
  label: string;
  icon: string;
}

export const CURATED_APPS: CuratedApp[] = [
  { pkg: "com.netflix.ninja", label: "Netflix", icon: "🎬" },
  { pkg: "com.google.android.youtube.tv", label: "YouTube", icon: "▶️" },
  { pkg: "com.amazon.avod.thirdpartyclient", label: "Prime Video", icon: "📺" },
  { pkg: "com.disney.disneyplus", label: "Disney+", icon: "🏰" },
  { pkg: "com.spotify.tv.android", label: "Spotify", icon: "🎵" },
  { pkg: "com.jixin.launcher5", label: "Home Launcher", icon: "🏠" },
];
