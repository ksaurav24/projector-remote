import type { CuratedApp } from "../apps/curatedApps";

export function AppIcon({ app, size = 44 }: { app: CuratedApp; size?: number }) {
  const style = { width: size, height: size };

  if (app.kind === "raster") {
    return (
      <img
        src={app.icon}
        alt=""
        style={style}
        className="rounded-2xl object-cover"
      />
    );
  }

  if (app.kind === "glyph") {
    return (
      <div
        style={{ ...style, backgroundColor: app.tint }}
        className="rounded-2xl flex items-center justify-center"
      >
        <img src={app.icon} alt="" className="w-1/2 h-1/2" />
      </div>
    );
  }

  return (
    <div
      style={style}
      className="rounded-2xl bg-neutral-100 dark:bg-neutral-800 flex items-center justify-center p-2"
    >
      <img src={app.icon} alt="" className="w-full h-full object-contain" />
    </div>
  );
}
