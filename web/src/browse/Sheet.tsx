import { motion, useDragControls } from "framer-motion";
import type { PointerEvent, ReactNode } from "react";

import { useDialogFocus } from "../hooks/useDialogFocus";

const DISMISS_OFFSET = 140;
const DISMISS_VELOCITY = 800;

/** A full-screen dark sheet that rises from the bottom. It can be
 * dragged down to dismiss, but only from the element that calls
 * startDrag (the artwork at the top), so it never fights the page's
 * own vertical scrolling. */
export function Sheet({
  onClose,
  children,
  compact = false,
}: {
  onClose: () => void;
  compact?: boolean;
  children: (startDrag: (e: PointerEvent) => void) => ReactNode;
}) {
  const controls = useDragControls();
  const panel = useDialogFocus(onClose);
  return (
    <motion.div
      ref={panel}
      tabIndex={-1}
      aria-label={compact ? "Now playing" : "Title details"}
      role="dialog"
      aria-modal
      initial={{ y: "100%" }}
      animate={{ y: 0 }}
      exit={{ y: "100%" }}
      transition={{ type: "spring", stiffness: 380, damping: 40 }}
      drag="y"
      dragListener={false}
      dragControls={controls}
      dragConstraints={{ top: 0, bottom: 0 }}
      dragElastic={{ top: 0, bottom: 0.7 }}
      onDragEnd={(_, info) => {
        if (info.offset.y > DISMISS_OFFSET || info.velocity.y > DISMISS_VELOCITY) onClose();
      }}
      className={`detail-panel ${compact ? "playback-panel" : ""} fixed inset-0 z-40 overflow-y-auto overscroll-contain bg-black text-bulb`}
    >
      {children((e) => controls.start(e))}
    </motion.div>
  );
}
