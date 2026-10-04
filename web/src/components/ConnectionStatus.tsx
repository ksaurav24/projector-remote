import { WifiHigh, WifiSlash } from "@phosphor-icons/react";

export function ConnectionStatus({ connected }: { connected: boolean }) {
  return (
    <div role="status" aria-live="polite" aria-atomic="true" className="flex items-center gap-1.5 text-xs font-medium">
      {connected ? (
        <WifiHigh aria-hidden="true" weight="bold" className="size-4 text-emerald-600 dark:text-emerald-400" />
      ) : (
        <WifiSlash aria-hidden="true" weight="bold" className="size-4 text-red-600 dark:text-red-400" />
      )}
      <span className={connected ? "text-neutral-600 dark:text-neutral-300" : "text-red-700 dark:text-red-300"}>
        {connected ? "Connected" : "Reconnecting"}
      </span>
    </div>
  );
}
