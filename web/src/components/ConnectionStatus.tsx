import { AnimatePresence, motion } from "framer-motion";

export function ConnectionStatus({ connected }: { connected: boolean }) {
  return (
    <AnimatePresence>
      {!connected && (
        <motion.div
          initial={{ y: -40, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -40, opacity: 0 }}
          transition={{ type: "spring", stiffness: 300, damping: 30 }}
          className="fixed top-0 left-0 right-0 z-50 bg-red-600 text-white text-center py-2 text-sm font-medium"
        >
          Projector offline — reconnecting…
        </motion.div>
      )}
    </AnimatePresence>
  );
}
