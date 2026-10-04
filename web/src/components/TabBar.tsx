import { motion } from "framer-motion";
import { FilmStrip, GameController, SlidersHorizontal } from "@phosphor-icons/react";

export type Tab = "remote" | "browse" | "settings";

const TABS: { id: Tab; label: string; Icon: typeof FilmStrip }[] = [
  { id: "remote", label: "Remote", Icon: GameController },
  { id: "browse", label: "Browse", Icon: FilmStrip },
  { id: "settings", label: "Settings", Icon: SlidersHorizontal },
];

/** Floating pill tab bar. On Browse it goes dark glass to sit in the
 * dark room; on Remote it follows the system theme like the rest of
 * that page. */
export function TabBar({ tab, onChange }: { tab: Tab; onChange: (t: Tab) => void }) {
  const dark = tab === "browse";
  return (
    <nav aria-label="Main navigation" className="app-nav pointer-events-none fixed inset-x-0 bottom-0 z-30 flex justify-center pb-[calc(env(safe-area-inset-bottom)+14px)]">
      <div
        className={
          "app-nav-inner pointer-events-auto flex rounded-full p-1.5 shadow-[0_12px_40px_-8px_rgba(0,0,0,0.5)] ring-1 backdrop-blur-xl " +
          (dark
            ? "bg-neutral-900/70 text-bulb ring-white/10"
            : "bg-white/80 text-neutral-900 ring-neutral-200 dark:bg-neutral-900/80 dark:text-neutral-100 dark:ring-white/10")
        }
      >
        <div className="nav-brand hidden lg:block">
          <span className="block text-xl font-bold tracking-tight">Projector</span>
          <span className="mt-1 block text-sm opacity-60">Home cinema remote</span>
        </div>
        {TABS.map(({ id, label, Icon }) => {
          const active = id === tab;
          return (
            <button
              key={id}
              onClick={() => onChange(id)}
              aria-label={label}
              aria-current={active ? "page" : undefined}
              className={
                "nav-item relative flex items-center gap-2 rounded-full px-3.5 sm:px-5 py-2.5 text-[14px] font-semibold transition-colors " +
                (active
                  ? "text-spot-ink"
                  : dark
                    ? "text-haze"
                    : "text-neutral-500 dark:text-neutral-400")
              }
            >
              {active && (
                <motion.span
                  layoutId="tab-pill"
                  className="absolute inset-0 rounded-full bg-spot"
                  transition={{ type: "spring", stiffness: 400, damping: 32 }}
                />
              )}
              <Icon weight={active ? "fill" : "bold"} className="relative size-[18px]" />
              <span className="relative hidden min-[390px]:inline lg:inline">{label}</span>
            </button>
          );
        })}
        <p className="mt-auto hidden px-3 text-xs leading-relaxed opacity-50 lg:block">Browse here.<br />Watch on the projector.</p>
      </div>
    </nav>
  );
}
