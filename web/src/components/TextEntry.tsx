import { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import { ArrowBendDownLeft, X } from "@phosphor-icons/react";
import { useWs } from "../ws/WsContext";

const DEBOUNCE_MS = 400;

/** A live-typing search field: as you type, the current value is
 * sent to the device's focused field after a short pause, instead of
 * requiring an explicit submit. internal/deviceio.Controller.Text is
 * diff-aware server-side (types only what changed, or backspaces and
 * retypes on an edit), so re-sending the full value on every pause is
 * safe and doesn't duplicate text on the TV. */
export function TextEntry() {
  const { send } = useWs();
  const [value, setValue] = useState("");
  const timer = useRef<number | null>(null);

  function scheduleSync(next: string) {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      send({ type: "text", value: next });
    }, DEBOUNCE_MS);
  }

  function onChange(e: React.ChangeEvent<HTMLInputElement>) {
    const next = e.target.value;
    setValue(next);
    scheduleSync(next);
  }

  function flushNow(next: string) {
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
    send({ type: "text", value: next });
  }

  function clear() {
    setValue("");
    flushNow("");
  }

  function submit() {
    send({ type: "key", code: "KEYCODE_ENTER" });
  }

  useEffect(() => {
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, []);

  return (
    <div className="flex items-center gap-2">
      <div className="relative flex-1">
        <input
          type="text"
          value={value}
          onChange={onChange}
          onBlur={() => flushNow(value)}
          placeholder="Type to search…"
          className="w-full rounded-2xl bg-white dark:bg-neutral-900 border border-neutral-200 dark:border-neutral-800 pl-4 pr-9 py-3 text-sm outline-none focus:border-orange-400 dark:focus:border-orange-500 transition-colors"
        />
        {value && (
          <button
            onClick={clear}
            aria-label="Clear"
            className="absolute right-2.5 top-1/2 -translate-y-1/2 text-neutral-400 hover:text-neutral-600 dark:hover:text-neutral-300"
          >
            <X weight="bold" className="size-4" />
          </button>
        )}
      </div>
      <motion.button
        whileTap={{ scale: 0.92 }}
        onClick={submit}
        aria-label="Go"
        className="shrink-0 rounded-2xl bg-orange-500 active:bg-orange-600 text-white p-3.5"
      >
        <ArrowBendDownLeft weight="bold" className="size-4" />
      </motion.button>
    </div>
  );
}
