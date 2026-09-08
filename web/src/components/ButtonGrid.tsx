import { useRef } from "react";
import { motion } from "framer-motion";
import { useWs } from "../ws/WsContext";

interface ButtonSpec {
  code: string;
  label: string;
}

const BUTTONS: ButtonSpec[] = [
  { code: "KEYCODE_BACK", label: "Back" },
  { code: "KEYCODE_DPAD_UP", label: "▲" },
  { code: "KEYCODE_HOME", label: "Home" },
  { code: "KEYCODE_DPAD_LEFT", label: "◀" },
  { code: "KEYCODE_DPAD_CENTER", label: "OK" },
  { code: "KEYCODE_DPAD_RIGHT", label: "▶" },
  { code: "KEYCODE_VOLUME_DOWN", label: "Vol−" },
  { code: "KEYCODE_DPAD_DOWN", label: "▼" },
  { code: "KEYCODE_VOLUME_UP", label: "Vol+" },
  { code: "KEYCODE_VOLUME_MUTE", label: "Mute" },
  { code: "KEYCODE_MEDIA_PLAY_PAUSE", label: "Play/Pause" },
  { code: "KEYCODE_POWER", label: "Power" },
];

const REPEAT_MS = 150;

function RemoteButton({ code, label }: ButtonSpec) {
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

  return (
    <motion.button
      whileTap={{ scale: 0.92 }}
      onPointerDown={start}
      onPointerUp={stop}
      onPointerCancel={stop}
      onPointerLeave={stop}
      className="rounded-xl bg-neutral-800 border border-neutral-700 py-3 text-sm font-medium active:bg-neutral-700"
    >
      {label}
    </motion.button>
  );
}

export function ButtonGrid() {
  return (
    <div className="grid grid-cols-3 gap-2 mt-4">
      {BUTTONS.map((b) => (
        <RemoteButton key={b.code} {...b} />
      ))}
    </div>
  );
}
