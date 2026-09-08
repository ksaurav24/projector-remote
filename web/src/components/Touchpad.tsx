import { useRef } from "react";
import { motion, useAnimationControls } from "framer-motion";
import { useWs } from "../ws/WsContext";

export function Touchpad() {
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
    controls.start({ scale: 0.98 });
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
      className="h-56 rounded-2xl bg-neutral-800 border border-neutral-700 touch-none select-none flex items-center justify-center text-neutral-500 text-sm"
    >
      Touchpad
    </motion.div>
  );
}
