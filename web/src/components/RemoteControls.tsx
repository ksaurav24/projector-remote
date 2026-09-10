import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { GameController, CursorClick } from "@phosphor-icons/react";
import { DpadCluster } from "./DpadCluster";
import { Trackpad } from "./Trackpad";

type Mode = "dpad" | "mouse";

export function RemoteControls() {
  const [mode, setMode] = useState<Mode>("dpad");

  return (
    <div>
      <div className="relative flex rounded-full bg-neutral-100 dark:bg-neutral-800 p-1 mb-4">
        <motion.div
          className="absolute inset-y-1 w-[calc(50%-4px)] rounded-full bg-white dark:bg-neutral-700 shadow-sm"
          animate={{ x: mode === "dpad" ? 0 : "100%" }}
          transition={{ type: "spring", stiffness: 400, damping: 32 }}
        />
        <button
          onClick={() => setMode("dpad")}
          className={
            "relative z-10 flex-1 flex items-center justify-center gap-1.5 py-2 text-sm font-medium transition-colors " +
            (mode === "dpad"
              ? "text-orange-600 dark:text-orange-400"
              : "text-neutral-500 dark:text-neutral-400")
          }
        >
          <GameController weight="bold" className="size-4" />
          D-pad
        </button>
        <button
          onClick={() => setMode("mouse")}
          className={
            "relative z-10 flex-1 flex items-center justify-center gap-1.5 py-2 text-sm font-medium transition-colors " +
            (mode === "mouse"
              ? "text-orange-600 dark:text-orange-400"
              : "text-neutral-500 dark:text-neutral-400")
          }
        >
          <CursorClick weight="bold" className="size-4" />
          Mouse
        </button>
      </div>

      <AnimatePresence mode="wait" initial={false}>
        <motion.div
          key={mode}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.15 }}
        >
          {mode === "dpad" ? <DpadCluster /> : <Trackpad />}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}
