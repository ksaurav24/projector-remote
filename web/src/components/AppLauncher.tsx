import { motion } from "framer-motion";
import { useMemo } from "react";
import { useWs } from "../ws/WsContext";
import { CURATED_APPS, FAVORITE_PKGS } from "../apps/curatedApps";
import { AppIcon } from "./AppIcon";

export function AppLauncher() {
  const { apps, send } = useWs();

  const installedSet = useMemo(() => new Set(apps), [apps]);
  const favoriteSet = useMemo(() => new Set(FAVORITE_PKGS), []);

  const curatedInstalled = CURATED_APPS.filter(
    (a) => installedSet.has(a.pkg) && !favoriteSet.has(a.pkg),
  );
  const curatedPkgs = new Set(CURATED_APPS.map((a) => a.pkg));
  const other = apps.filter((pkg) => !curatedPkgs.has(pkg) && !favoriteSet.has(pkg));

  function launch(pkg: string) {
    send({ type: "launch", pkg });
  }

  return (
    <div>
      <h2 className="text-xs font-semibold uppercase tracking-wide text-neutral-400 dark:text-neutral-500 mb-2">
        Apps
      </h2>
      <div className="grid grid-cols-4 gap-3">
        {curatedInstalled.map((a) => (
          <motion.button
            key={a.pkg}
            whileTap={{ scale: 0.92 }}
            onClick={() => launch(a.pkg)}
            className="flex flex-col items-center gap-1.5"
          >
            <AppIcon app={a} size={52} />
            <span className="text-[11px] text-neutral-600 dark:text-neutral-400 truncate max-w-full">
              {a.label}
            </span>
          </motion.button>
        ))}
      </div>
      {other.length > 0 && (
        <div className="mt-3 max-h-40 overflow-y-auto rounded-2xl bg-white dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 divide-y divide-neutral-100 dark:divide-neutral-800">
          {other.map((pkg) => (
            <button
              key={pkg}
              onClick={() => launch(pkg)}
              className="w-full text-left px-3 py-2 text-xs text-neutral-500 dark:text-neutral-400 active:bg-neutral-50 dark:active:bg-neutral-800 transition-colors"
            >
              {pkg}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
