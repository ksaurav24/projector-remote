import { useRef } from "react";
import { useWs } from "../ws/WsContext";

const REPEAT_MS = 150;

/** Press-and-hold repeat for a discrete remote key, matching how a
 * physical remote's buttons behave (there's no virtual key-hold
 * primitive on the device, so repeat is simulated client-side). */
export function useKeyRepeat(code: string) {
  const { send } = useWs();
  const timer = useRef<number | null>(null);

  function press() {
    send({ type: "key", code });
  }

  function start() {
    press();
    timer.current = window.setInterval(press, REPEAT_MS);
  }

  function stop() {
    if (timer.current !== null) {
      window.clearInterval(timer.current);
      timer.current = null;
    }
  }

  return {
    onPointerDown: start,
    onPointerUp: stop,
    onPointerCancel: stop,
    onPointerLeave: stop,
  };
}
