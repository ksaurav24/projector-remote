import { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import { ArrowBendDownLeft, Check, MagnifyingGlass, WifiSlash, X } from "@phosphor-icons/react";
import { useWs } from "../ws/WsContext";

const DEBOUNCE_MS = 400;

/** A live-typing search field: as you type, the current value is
 * sent to the device's focused field after a short pause, instead of
 * requiring an explicit submit. internal/deviceio.Controller.Text is
 * diff-aware server-side (types only what changed, or backspaces and
 * retypes on an edit), so re-sending the full value on every pause is
 * safe and doesn't duplicate text on the TV. */
export function TextEntry() {
  const { connected, send } = useWs();
  const [value, setValue] = useState("");
  const [focused, setFocused] = useState(false);
  const [delivery, setDelivery] = useState<"idle" | "typing" | "queued" | "sent">("idle");
  const timer = useRef<number | null>(null);
  const pendingValue = useRef<string | null>(null);
  const sendRef = useRef(send);

  useEffect(() => { sendRef.current = send; }, [send]);

  function scheduleSync(next: string) {
    if (timer.current !== null) window.clearTimeout(timer.current);
    pendingValue.current = next;
    setDelivery(connected ? "typing" : "queued");
    timer.current = window.setTimeout(() => {
      timer.current = null;
      if (!connected) {
        setDelivery("queued");
        return;
      }
      send({ type: "text", value: next });
      pendingValue.current = null;
      setDelivery("sent");
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
    pendingValue.current = next;
    if (!connected) {
      setDelivery("queued");
      return;
    }
    send({ type: "text", value: next });
    pendingValue.current = null;
    setDelivery("sent");
  }

  function clear() {
    setValue("");
    flushNow("");
  }

  function submit() {
    if (!connected || !value.trim()) return;
    flushNow(value);
    send({ type: "key", code: "KEYCODE_ENTER" });
  }

  useEffect(() => {
    if (!connected || pendingValue.current === null) return;
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
    sendRef.current({ type: "text", value: pendingValue.current });
    pendingValue.current = null;
    setDelivery("sent");
  }, [connected]);

  useEffect(() => {
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, []);

  return (
    <div className="space-y-2.5">
      <label htmlFor="projector-text-entry" className={"block text-sm font-semibold transition-colors " + (focused ? "text-orange-600 dark:text-orange-400" : "text-neutral-800 dark:text-neutral-200")}>
        Type to search on your projector
      </label>
      <div className="flex items-center gap-2">
        <div className={"relative flex min-w-0 flex-1 items-center rounded-2xl border bg-white transition-[border-color,box-shadow] dark:bg-neutral-900 " +
          (focused
            ? "border-orange-400 ring-2 ring-orange-400/25 dark:border-orange-500 dark:ring-orange-500/25"
            : "border-neutral-200 dark:border-neutral-800")}>
          <MagnifyingGlass aria-hidden="true" weight="bold" className={"ml-4 size-4 shrink-0 transition-colors " + (focused ? "text-orange-500" : "text-neutral-400")} />
          <input
            id="projector-text-entry"
            type="text"
            inputMode="search"
            autoComplete="off"
            aria-describedby="projector-text-help projector-text-status"
            onKeyDown={(event) => { if (event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); submit(); } }}
            value={value}
            onChange={onChange}
            onFocus={() => setFocused(true)}
            onBlur={() => { setFocused(false); flushNow(value); }}
            placeholder="Search or enter text…"
            className="min-w-0 flex-1 rounded-2xl bg-transparent py-3.5 pl-3 pr-1 text-base outline-none placeholder:text-neutral-400 dark:placeholder:text-neutral-500"
          />
          {value && (
            <button
              onClick={clear}
              type="button"
              aria-label="Clear text"
              className="mr-1 grid size-11 shrink-0 place-items-center rounded-xl text-neutral-500 transition-colors hover:bg-neutral-100 hover:text-neutral-800 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
            >
              <X aria-hidden="true" weight="bold" className="size-4" />
            </button>
          )}
        </div>
        <motion.button
          type="button"
          whileTap={{ scale: 0.92 }}
          onClick={submit}
          aria-label="Send text and press Enter on projector"
          disabled={!connected || !value.trim()}
          className="grid size-12 shrink-0 place-items-center rounded-2xl bg-orange-500 text-white transition-colors hover:bg-orange-600 active:bg-orange-700 disabled:cursor-not-allowed disabled:bg-neutral-300 disabled:text-neutral-500 dark:disabled:bg-neutral-800 dark:disabled:text-neutral-500"
        >
          <ArrowBendDownLeft aria-hidden="true" weight="bold" className="size-4" />
        </motion.button>
      </div>
      <div className="flex min-h-5 items-center justify-between gap-3 text-xs">
        <p id="projector-text-help" className="min-w-0 text-neutral-500 dark:text-neutral-400">
          {connected ? "Sends to the field currently focused on the projector." : "Reconnect to send text; your draft is kept here."}
        </p>
        <span id="projector-text-status" aria-live="polite" className="inline-flex shrink-0 items-center gap-1 font-medium">
          {!connected ? <><WifiSlash aria-hidden="true" className="size-3.5 text-red-500" /><span className="text-red-600 dark:text-red-400">Offline</span></> :
            delivery === "queued" ? <span className="text-orange-600 dark:text-orange-400">Queued</span> :
            delivery === "sent" ? <><Check aria-hidden="true" className="size-3.5 text-emerald-600 dark:text-emerald-400" /><span className="text-emerald-700 dark:text-emerald-400">Sent</span></> :
            delivery === "typing" ? <span className="text-orange-600 dark:text-orange-400">Typing…</span> : null}
        </span>
      </div>
    </div>
  );
}
