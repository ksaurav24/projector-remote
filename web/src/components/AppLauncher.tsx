import { motion } from "framer-motion";
import { useMemo } from "react";
import { useWs } from "../ws/WsContext";
import { CURATED_APPS } from "../apps/curatedApps";

export function AppLauncher() {
  const { apps, send } = useWs();

  const installedSet = useMemo(() => new Set(apps), [apps]);
  const curatedInstalled = CURATED_APPS.filter((a) => installedSet.has(a.pkg));
  const curatedPkgs = new Set(CURATED_APPS.map((a) => a.pkg));
  const other = apps.filter((pkg) => !curatedPkgs.has(pkg));

  function launch(pkg: string) {
    send({ type: "launch", pkg });
  }

  return (
    <div className="mt-6">
      <h2 className="text-sm font-medium text-neutral-400 mb-2">Apps</h2>
      <div className="grid grid-cols-3 gap-2">
        {curatedInstalled.map((a) => (
          <motion.button
            key={a.pkg}
            whileTap={{ scale: 0.94 }}
            onClick={() => launch(a.pkg)}
            className="rounded-xl bg-neutral-800 border border-neutral-700 py-3 flex flex-col items-center gap-1 text-xs"
          >
            <span className="text-xl">{a.icon}</span>
            {a.label}
          </motion.button>
        ))}
      </div>
      {other.length > 0 && (
        <div className="mt-3 max-h-40 overflow-y-auto rounded-xl bg-neutral-900 border border-neutral-800 divide-y divide-neutral-800">
          {other.map((pkg) => (
            <button
              key={pkg}
              onClick={() => launch(pkg)}
              className="w-full text-left px-3 py-2 text-xs text-neutral-400 hover:bg-neutral-800"
            >
              {pkg}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
