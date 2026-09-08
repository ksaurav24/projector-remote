import { useState, type FormEvent } from "react";
import { useWs } from "../ws/WsContext";

export function TextEntry() {
  const { send } = useWs();
  const [value, setValue] = useState("");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = value.trim();
    if (!trimmed) return;
    send({ type: "text", value: trimmed });
    setValue("");
  }

  return (
    <form onSubmit={onSubmit} className="mt-4 flex gap-2">
      <input
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="Type to search…"
        className="flex-1 rounded-xl bg-neutral-800 border border-neutral-700 px-3 py-2 text-sm outline-none focus:border-neutral-500"
      />
      <button
        type="submit"
        className="rounded-xl bg-neutral-100 text-neutral-900 px-4 py-2 text-sm font-medium"
      >
        Send
      </button>
    </form>
  );
}
