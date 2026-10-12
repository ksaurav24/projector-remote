import { useEffect, useRef, useState } from "react";
import type { Icon } from "@phosphor-icons/react";
import {
  ArrowCounterClockwise,
  CaretLeft,
  CaretRight,
  Crosshair,
  CubeFocus,
  MagicWand,
  ProjectorScreen,
  SlidersHorizontal,
  SkipForward,
  WarningCircle,
} from "@phosphor-icons/react";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { useWs } from "../ws/WsContext";
import type { Corner, CornerPoint, ProjectorSettings } from "../ws/client";
import type { SkipMode } from "../playback/types";

const FEATURES = [
  ["autoFocus", "Auto focus", "Refocus when the projector or screen moves"],
  ["trapezoidAuto", "Auto keystone", "Straighten the projected image automatically"],
  ["autoBalance", "Auto balance", "Keep the image level with the room"],
  ["autoAvoidance", "Obstacle avoidance", "Move the image away from objects"],
  ["autoCurtain", "Screen detection", "Fit the image inside a projection screen"],
  ["trapezoidTB", "Vertical correction", "Enable top and bottom keystone correction"],
] as const;

const MODES: { id: ProjectorSettings["mode"]; title: string; caption: string }[] = [
  { id: "front", title: "Front", caption: "Table mounted" },
  { id: "rear", title: "Rear", caption: "Behind the screen" },
  { id: "ceiling-front", title: "Ceiling", caption: "Front ceiling" },
  { id: "ceiling-rear", title: "Rear ceiling", caption: "Behind and inverted" },
];

function Card({
  icon: CardIcon,
  title,
  caption,
  children,
  className = "",
}: {
  icon: Icon;
  title: string;
  caption: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={`rounded-[24px] border border-neutral-200 bg-white p-5 shadow-[0_14px_40px_-28px_rgba(0,0,0,.32)] dark:border-neutral-800 dark:bg-neutral-900 ${className}`}>
      <div className="mb-5 flex items-start gap-3">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-orange-50 text-orange-600 dark:bg-orange-950/50 dark:text-orange-400">
          <CardIcon weight="bold" className="size-5" />
        </div>
        <div>
          <h2 className="font-semibold leading-5">{title}</h2>
          <p className="mt-1 text-sm leading-5 text-neutral-500 dark:text-neutral-400">{caption}</p>
        </div>
      </div>
      {children}
    </section>
  );
}

function RangeControl({
  label,
  value,
  min,
  max,
  suffix,
  live = false,
  onChange,
  onCommit,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  suffix: string;
  live?: boolean;
  onChange: (value: number) => void;
  onCommit: (value: number) => void;
}) {
  const dirty = useRef(false);
  const latest = useRef(value);
  const lastSent = useRef(value);
  const timer = useRef<number | null>(null);

  useEffect(() => {
    latest.current = value;
    if (!dirty.current) lastSent.current = value;
  }, [value]);

  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
  }, []);

  function sendLatest() {
    timer.current = null;
    if (latest.current === lastSent.current) return;
    lastSent.current = latest.current;
    onCommit(latest.current);
  }

  function commit() {
    if (!dirty.current) return;
    dirty.current = false;
    if (timer.current !== null) window.clearTimeout(timer.current);
    sendLatest();
  }

  return (
    <label className="block">
      <span className="mb-3 flex items-center justify-between text-sm font-medium">
        {label}
        <span className="rounded-lg bg-neutral-100 px-2.5 py-1 font-mono text-sm tabular-nums text-neutral-700 dark:bg-neutral-800 dark:text-neutral-200">
          {value}{suffix}
        </span>
      </span>
      <input
        type="range"
        min={min}
        max={max}
        value={value}
        onChange={(event) => {
          dirty.current = true;
          const next = Number(event.target.value);
          latest.current = next;
          onChange(next);
          if (live && timer.current === null) timer.current = window.setTimeout(sendLatest, 100);
        }}
        onPointerUp={commit}
        onKeyUp={commit}
        onBlur={commit}
        className="settings-range w-full"
      />
      <span className="mt-1.5 flex justify-between text-xs text-neutral-400">
        <span>{min}{suffix}</span>
        <span>{max}{suffix}</span>
      </span>
    </label>
  );
}

function FocusButton({ direction, label, Icon }: { direction: "near" | "far"; label: string; Icon: Icon }) {
  const { send, connected } = useWs();
  const timer = useRef<number | null>(null);
  const active = useRef(false);

  function stop() {
    if (!active.current) return;
    active.current = false;
    if (timer.current !== null) window.clearInterval(timer.current);
    timer.current = null;
    send({ type: "focus_stop" });
  }

  function start() {
    if (!connected || active.current) return;
    active.current = true;
    send({ type: "focus_start", direction });
    timer.current = window.setInterval(() => send({ type: "focus_start", direction }), 500);
  }

  useEffect(() => {
    const onBlur = () => stop();
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("blur", onBlur);
      stop();
    };
  }, []);

  return (
    <button
      disabled={!connected}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        start();
      }}
      onPointerUp={stop}
      onPointerCancel={stop}
      onLostPointerCapture={stop}
      onKeyDown={(event) => {
        if ((event.key === "Enter" || event.key === " ") && !event.repeat) start();
      }}
      onKeyUp={(event) => {
        if (event.key === "Enter" || event.key === " ") stop();
      }}
      className="flex min-h-20 flex-1 touch-none select-none flex-col items-center justify-center gap-1 rounded-2xl border border-neutral-200 bg-neutral-50 font-semibold text-neutral-700 transition active:scale-[.98] active:border-orange-300 active:bg-orange-50 active:text-orange-700 disabled:opacity-40 dark:border-neutral-700 dark:bg-neutral-800 dark:text-neutral-200 dark:active:border-orange-700 dark:active:bg-orange-950/40 dark:active:text-orange-300"
    >
      <Icon weight="bold" className="size-5" />
      {label}
      <span className="text-[11px] font-normal text-neutral-400">Press and hold</span>
    </button>
  );
}

const CORNERS: Corner[] = ["lt", "rt", "rb", "lb"];
const MAPPER_INSET = 0.07;
const CORNER_LABELS: Record<Corner, string> = {
  lt: "Top left",
  rt: "Top right",
  rb: "Bottom right",
  lb: "Bottom left",
};
const MODE_CORNER_MAP: Record<ProjectorSettings["mode"], Record<Corner, Corner>> = {
  front: { lt: "lt", rt: "rt", rb: "rb", lb: "lb" },
  rear: { lt: "rt", rt: "lt", rb: "lb", lb: "rb" },
  "ceiling-front": { lt: "rb", rt: "lb", rb: "lt", lb: "rt" },
  "ceiling-rear": { lt: "lb", rt: "rb", rb: "rt", lb: "lt" },
};

function orientedCorners(points: Record<Corner, CornerPoint>, mode: ProjectorSettings["mode"]): Record<Corner, CornerPoint> {
  const mapping = MODE_CORNER_MAP[mode];
  return {
    lt: points[mapping.lt],
    rt: points[mapping.rt],
    rb: points[mapping.rb],
    lb: points[mapping.lb],
  };
}

function mapperPosition(value: number): number {
  return (MAPPER_INSET + value * (1 - MAPPER_INSET * 2)) * 100;
}

function CornerMapper({
  points,
  maxX,
  maxY,
  viewLabel,
  onChange,
  onReset,
}: {
  points: Record<Corner, CornerPoint>;
  maxX: number;
  maxY: number;
  viewLabel: string;
  onChange: (corner: Corner, point: CornerPoint, final: boolean) => void;
  onReset: () => void;
}) {
  const [selected, setSelected] = useState<Corner>("lt");
  const [local, setLocal] = useState(points);
  const surface = useRef<HTMLDivElement | null>(null);
  const dragging = useRef<Corner | null>(null);
  const queued = useRef<{ corner: Corner; point: CornerPoint; final: boolean } | null>(null);
  const timer = useRef<number | null>(null);

  useEffect(() => {
    if (!dragging.current) setLocal(points);
  }, [points]);

  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
  }, []);

  function flush() {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
    const update = queued.current;
    queued.current = null;
    if (update) onChange(update.corner, update.point, update.final);
  }

  function queue(corner: Corner, point: CornerPoint) {
    queued.current = { corner, point, final: false };
    if (timer.current !== null) return;
    timer.current = window.setTimeout(flush, 100);
  }

  function pointFromPointer(event: React.PointerEvent, corner: Corner): CornerPoint {
    const rect = surface.current!.getBoundingClientRect();
    const width = 1 - MAPPER_INSET * 2;
    const px = Math.min(1, Math.max(0, ((event.clientX - rect.left) / rect.width - MAPPER_INSET) / width));
    const py = Math.min(1, Math.max(0, ((event.clientY - rect.top) / rect.height - MAPPER_INSET) / width));
    return {
      x: Math.round((corner === "lt" || corner === "lb" ? px : 1 - px) * maxX),
      y: Math.round((corner === "lt" || corner === "rt" ? py : 1 - py) * maxY),
    };
  }

  function move(event: React.PointerEvent<HTMLButtonElement>, corner: Corner) {
    if (dragging.current !== corner) return;
    const point = pointFromPointer(event, corner);
    setLocal((current) => ({ ...current, [corner]: point }));
    queue(corner, point);
  }

  function finish(event: React.PointerEvent<HTMLButtonElement>, corner: Corner) {
    if (dragging.current !== corner) return;
    const point = pointFromPointer(event, corner);
    setLocal((current) => ({ ...current, [corner]: point }));
    queued.current = { corner, point, final: true };
    dragging.current = null;
    flush();
  }

  const polygon = CORNERS.map((corner) => {
    const point = local[corner];
    const x = mapperPosition(corner === "lt" || corner === "lb" ? point.x / maxX : 1 - point.x / maxX);
    const y = mapperPosition(corner === "lt" || corner === "rt" ? point.y / maxY : 1 - point.y / maxY);
    return `${x},${y}`;
  }).join(" ");

  return (
    <div className="grid gap-5 sm:grid-cols-[minmax(0,1fr)_184px] sm:items-center">
      <div ref={surface} className="relative mx-auto aspect-video w-full max-w-lg touch-none overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950 shadow-inner">
        <svg viewBox="0 0 100 100" preserveAspectRatio="none" className="pointer-events-none absolute inset-0 size-full" aria-hidden="true">
          <polygon points={polygon} className="fill-orange-500/15 stroke-orange-400" vectorEffect="non-scaling-stroke" strokeWidth="2" />
        </svg>
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-white/50">Drag any corner</span>
        {CORNERS.map((corner) => {
          const point = local[corner];
          const left = mapperPosition(corner === "lt" || corner === "lb" ? point.x / maxX : 1 - point.x / maxX);
          const top = mapperPosition(corner === "lt" || corner === "rt" ? point.y / maxY : 1 - point.y / maxY);
          const active = selected === corner;
          return (
            <button
              key={corner}
              aria-label={`Drag ${corner.toUpperCase()} corner. X ${point.x}, Y ${point.y}`}
              aria-pressed={active}
              style={{ left: `${left}%`, top: `${top}%` }}
              onPointerDown={(event) => {
                event.currentTarget.setPointerCapture(event.pointerId);
                dragging.current = corner;
                setSelected(corner);
              }}
              onPointerMove={(event) => move(event, corner)}
              onPointerUp={(event) => finish(event, corner)}
              onPointerCancel={(event) => finish(event, corner)}
              className={`absolute z-10 flex size-11 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border-2 text-[10px] font-bold uppercase shadow-lg transition-[background-color,border-color,box-shadow] ${active ? "border-orange-300 bg-orange-500 text-white shadow-orange-500/30" : "border-white/50 bg-neutral-900 text-white/75"}`}
            >
              {corner}
            </button>
          );
        })}
      </div>

      <div className="space-y-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[.16em] text-neutral-400">{CORNER_LABELS[selected]}</p>
          <p className="mt-1 text-xs text-neutral-500">{viewLabel} view · zero is the outer edge</p>
        </div>
        <div className="grid grid-cols-2 gap-2">
          {(["x", "y"] as const).map((axis) => (
            <output key={axis} className="rounded-xl border border-neutral-200 bg-neutral-50 p-3 dark:border-neutral-700 dark:bg-neutral-800">
              <span className="block text-[10px] font-semibold uppercase tracking-wider text-neutral-400">{axis}</span>
              <span className="mt-0.5 block font-mono text-lg font-semibold tabular-nums">{local[selected][axis]}</span>
              <span className="block text-[10px] text-neutral-400">0–{axis === "x" ? maxX : maxY}</span>
            </output>
          ))}
        </div>
        <button onClick={onReset} className="flex min-h-11 w-full items-center justify-center gap-2 rounded-xl border border-neutral-200 bg-neutral-50 text-sm font-semibold text-orange-600 transition active:scale-[.98] dark:border-neutral-700 dark:bg-neutral-800 dark:text-orange-400">
          <ArrowCounterClockwise weight="bold" /> Reset mapping
        </button>
      </div>
    </div>
  );
}

function LoadingCards() {
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {[0, 1, 2, 3].map((key) => <div key={key} className="h-56 animate-pulse rounded-[24px] border border-neutral-200 bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-900" />)}
    </div>
  );
}

const SKIP_MODES: { id: SkipMode; label: string }[] = [
  { id: "manual", label: "Manual" },
  { id: "auto", label: "Automatic" },
  { id: "off", label: "Off" },
];

function AnimeSkipCard() {
  const { connected, animeSkipSettings: settings, animeSkipError: error, animeSkipSaving: saving, send } = useWs();
  return (
    <Card icon={SkipForward} title="Anime intros and outros" caption="Choose how you skip songs when timings are available." className="mb-5">
      <div className="grid gap-5 sm:grid-cols-2">
        {(["intro", "outro"] as const).map((kind) => (
          <fieldset key={kind} disabled={!connected || !settings || saving}>
            <legend className="mb-2 text-sm font-semibold">{kind === "intro" ? "Intro" : "Outro"}</legend>
            <div className="grid grid-cols-3 gap-2">
              {SKIP_MODES.map(({ id, label }) => (
                <button
                  type="button"
                  key={id}
                  aria-pressed={settings?.[kind] === id}
                  onClick={() => send({ type: "set_anime_skip", kind, mode: id })}
                  className={`min-h-12 rounded-xl border px-2 text-sm font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-orange-500 disabled:opacity-50 ${settings?.[kind] === id ? "border-orange-400 bg-orange-50 text-orange-800 dark:border-orange-600 dark:bg-orange-950/40 dark:text-orange-200" : "border-neutral-200 bg-neutral-50 text-neutral-700 active:bg-neutral-200 dark:border-neutral-700 dark:bg-neutral-800 dark:text-neutral-200 dark:active:bg-neutral-700"}`}
                >{label}</button>
              ))}
            </div>
          </fieldset>
        ))}
      </div>
      <p className="mt-4 text-sm leading-5 text-neutral-600 dark:text-neutral-300">Manual shows a skip button. Automatic skips for you. Off hides skip controls. Outro skips keep any scenes after the credits.</p>
      <p role="status" aria-live="polite" className={`mt-2 min-h-5 text-sm ${error ? "text-red-700 dark:text-red-300" : "text-neutral-600 dark:text-neutral-300"}`}>
        {error ?? (saving ? "Saving…" : !connected ? "Reconnect to change your preferences." : !settings ? "Loading preferences…" : "Saved on your projector. Changes apply to the current episode too.")}
      </p>
    </Card>
  );
}

export function SettingsPage() {
  const { connected, projectorSettings: settings, settingsError, send } = useWs();
  const [zoom, setZoom] = useState(100);
  const [horizontal, setHorizontal] = useState(0);
  const [vertical, setVertical] = useState(0);

  useEffect(() => {
    if (!settings) return;
    setZoom(settings.zoom);
    setHorizontal(settings.horizontal);
    setVertical(settings.vertical);
  }, [settings]);

  return (
    <div className="min-h-full bg-neutral-50 text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
      <header className="sticky top-0 z-20 border-b border-neutral-200 bg-neutral-50/90 px-4 pb-3 pt-[calc(env(safe-area-inset-top)+12px)] backdrop-blur-xl dark:border-neutral-900 dark:bg-neutral-950/90 md:px-8">
        <div className="mx-auto flex max-w-6xl items-center justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[.18em] text-orange-500">Projector</p>
            <h1 className="text-lg font-bold">Settings</h1>
          </div>
          <ConnectionStatus connected={connected} />
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-4 pb-44 pt-5 md:px-8 md:pt-8">
        <AnimeSkipCard />
        {settingsError && (
          <div className="mb-4 flex items-start gap-3 rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
            <WarningCircle weight="fill" className="mt-0.5 size-5 shrink-0" />
            <div><strong className="block">Settings unavailable</strong>{settingsError}</div>
          </div>
        )}

        {!settings ? <LoadingCards /> : (
          <div className="grid items-start gap-4 lg:grid-cols-2">
            <Card icon={ProjectorScreen} title="Screen size" caption="Resize the whole picture without changing its shape.">
              <RangeControl
                label="Projected area"
                value={zoom}
                min={settings.minZoom}
                max={100}
                suffix="%"
                live
                onChange={setZoom}
                onCommit={(value) => send({ type: "set_zoom", percent: value })}
              />
            </Card>

            <Card icon={CubeFocus} title="Manual focus" caption="Hold a button while watching the projected image.">
              <div className="flex gap-3">
                <FocusButton direction="near" label="Near" Icon={CaretLeft} />
                <FocusButton direction="far" label="Far" Icon={CaretRight} />
              </div>
            </Card>

            <Card icon={Crosshair} title="Four-corner mapping" caption="Drag each corner while watching the projected image." className="lg:col-span-2">
              <CornerMapper
                maxX={settings.cornerMaxX}
                maxY={settings.cornerMaxY}
                viewLabel={MODES.find((mode) => mode.id === settings.mode)?.title ?? "Front"}
                points={orientedCorners(settings.corners, settings.mode)}
                onChange={(corner, point, final) => send({ type: "set_corner", corner: MODE_CORNER_MAP[settings.mode][corner], x: point.x, y: point.y, final })}
                onReset={() => send({ type: "reset_keystone" })}
              />
            </Card>

            <Card icon={SlidersHorizontal} title="Manual keystone" caption="Correct the overall horizontal and vertical angle.">
              <div className="space-y-6">
                <RangeControl label="Horizontal" value={horizontal} min={-40} max={40} suffix="°" onChange={setHorizontal} onCommit={(value) => send({ type: "set_keystone", axis: "horizontal", angle: value })} />
                <RangeControl label="Vertical" value={vertical} min={-40} max={40} suffix="°" onChange={setVertical} onCommit={(value) => send({ type: "set_keystone", axis: "vertical", angle: value })} />
              </div>
            </Card>

            <Card icon={ProjectorScreen} title="Projection position" caption="Match the physical placement of the projector.">
              <div className="grid grid-cols-2 gap-2">
                {MODES.map((mode) => {
                  const active = settings.mode === mode.id;
                  return (
                    <button
                      key={mode.id}
                      onClick={() => send({ type: "set_projection_mode", mode: mode.id })}
                      aria-pressed={active}
                      className={`rounded-2xl border p-3 text-left transition active:scale-[.98] ${active ? "border-orange-400 bg-orange-50 text-orange-800 dark:border-orange-600 dark:bg-orange-950/40 dark:text-orange-200" : "border-neutral-200 bg-neutral-50 text-neutral-700 dark:border-neutral-700 dark:bg-neutral-800 dark:text-neutral-200"}`}
                    >
                      <span className="block text-sm font-semibold">{mode.title}</span>
                      <span className="mt-0.5 block text-xs opacity-60">{mode.caption}</span>
                    </button>
                  );
                })}
              </div>
            </Card>

            <Card icon={MagicWand} title="Automatic adjustments" caption="Choose what the projector may correct on its own." className="lg:col-span-2">
              <div className="grid gap-x-8 sm:grid-cols-2">
                {FEATURES.map(([key, label, caption]) => (
                  <label key={key} className="flex cursor-pointer items-center gap-3 border-b border-neutral-100 py-3 last:border-b-0 dark:border-neutral-800 sm:[&:nth-last-child(-n+2)]:border-b-0">
                    <span className="min-w-0 flex-1">
                      <span className="block text-sm font-medium">{label}</span>
                      <span className="block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{caption}</span>
                    </span>
                    <input
                      type="checkbox"
                      checked={Boolean(settings.features[key])}
                      onChange={(event) => send({ type: "set_projection_feature", key, enabled: event.target.checked })}
                      className="size-5 shrink-0 accent-orange-500"
                    />
                  </label>
                ))}
              </div>
            </Card>
          </div>
        )}
      </main>
    </div>
  );
}
