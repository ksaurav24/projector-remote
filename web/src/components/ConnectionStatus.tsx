import { AnimatePresence, motion } from "framer-motion";
import { WifiHigh, WifiSlash } from "@phosphor-icons/react";

export function ConnectionStatus({ connected }: { connected: boolean }) {
  return (
    <div className="flex items-center gap-1.5 text-xs font-medium">
      {connected ? (
        <WifiHigh weight="bold" className="size-4 text-orange-500" />
      ) : (
        <motion.span
          animate={{ opacity: [1, 0.4, 1] }}
          transition={{ duration: 1.4, repeat: Infinity, ease: "easeInOut" }}
        >
          <WifiSlash weight="bold" className="size-4 text-red-500" />
        </motion.span>
      )}
      <AnimatePresence mode="wait">
        <motion.span
          key={connected ? "on" : "off"}
          initial={{ opacity: 0, y: -4 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 4 }}
          transition={{ duration: 0.2 }}
          className={connected ? "text-neutral-500 dark:text-neutral-400" : "text-red-500"}
        >
          {connected ? "Connected" : "Reconnecting"}
        </motion.span>
      </AnimatePresence>
    </div>
  );
}
