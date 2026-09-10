import { useRef } from "react";
import { motion, useAnimationControls } from "framer-motion";
import { CursorClick } from "@phosphor-icons/react";
import { useWs } from "../ws/WsContext";

/** A full-size touch surface that drives the device's pointer
 * directly: each drag is forwarded as relative dx/dy, which
 * internal/deviceio writes straight into the kernel's real pointer
 * evdev node - Android can't tell it apart from a physical mouse. A
 * tap (no meaningful movement between down and up) sends a click. */
export function Trackpad() {
  const { send } = useWs();
  const controls = useAnimationControls();
  const lastPos = useRef<{ x: number; y: number } | null>(null);
  const pending = useRef({ dx: 0, dy: 0 });
  const rafId = useRef<number | null>(null);
  const moved = useRef(false);

  function flush() {
    rafId.current = null;
    const { dx, dy } = pending.current;
    if (dx !== 0 || dy !== 0) {
      send({ type: "move", dx: Math.round(dx), dy: Math.round(dy) });
      pending.current = { dx: 0, dy: 0 };
    }
  }

  function schedule() {
    if (rafId.current === null) {
      rafId.current = requestAnimationFrame(flush);
    }
  }

  function onPointerDown(e: React.PointerEvent) {
    (e.target as Element).setPointerCapture(e.pointerId);
    lastPos.current = { x: e.clientX, y: e.clientY };
    moved.current = false;
    controls.start({ scale: 0.99 });
  }

  function onPointerMove(e: React.PointerEvent) {
    if (!lastPos.current) return;
    const dx = e.clientX - lastPos.current.x;
    const dy = e.clientY - lastPos.current.y;
    lastPos.current = { x: e.clientX, y: e.clientY };
    if (Math.abs(dx) > 0.5 || Math.abs(dy) > 0.5) moved.current = true;
    pending.current.dx += dx;
    pending.current.dy += dy;
    schedule();
  }

  function onPointerUp() {
    lastPos.current = null;
    controls.start({ scale: 1 });
    if (!moved.current) {
      send({ type: "pointerdown" });
      send({ type: "pointerup" });
    }
  }

  return (
    <motion.div
      animate={controls}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      className="relative h-72 rounded-3xl bg-neutral-100 dark:bg-neutral-800 border border-neutral-200 dark:border-neutral-700 touch-none select-none flex flex-col items-center justify-center gap-2 text-neutral-400 dark:text-neutral-500"
    >
      <CursorClick weight="light" className="size-7" />
      <span className="text-xs font-medium">Drag to move &middot; Tap to click</span>
    </motion.div>
  );
}
