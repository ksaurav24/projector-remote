import { motion } from "framer-motion";
import { useMemo } from "react";
import { useWs } from "../ws/WsContext";
import { CURATED_APPS, FAVORITE_PKGS } from "../apps/curatedApps";
import { AppIcon } from "./AppIcon";

export function FavoritesRow() {
  const { send } = useWs();

  const favorites = useMemo(
    () =>
      FAVORITE_PKGS.map((pkg) => CURATED_APPS.find((a) => a.pkg === pkg)).filter(
        (a) => a !== undefined,
      ),
    [],
  );

  return (
    <div className="grid grid-cols-2 gap-3">
      {favorites.map((app) => (
        <motion.button
          key={app.pkg}
          whileTap={{ scale: 0.95 }}
          onClick={() => send({ type: "launch", pkg: app.pkg })}
          className="flex items-center gap-3 rounded-2xl bg-white dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 p-3 text-left shadow-sm active:shadow-none transition-shadow"
        >
          <AppIcon app={app} size={48} />
          <span className="font-semibold text-sm text-neutral-900 dark:text-neutral-100 truncate">
            {app.label}
          </span>
        </motion.button>
      ))}
    </div>
  );
}
