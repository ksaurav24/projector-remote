import { useWs } from "./ws/WsContext";
import { ConnectionStatus } from "./components/ConnectionStatus";
import { RemoteControls } from "./components/RemoteControls";
import { VolumeSlider } from "./components/VolumeSlider";
import { TextEntry } from "./components/TextEntry";
import { FavoritesRow } from "./components/FavoritesRow";
import { AppLauncher } from "./components/AppLauncher";

export function RemotePage() {
  const { connected } = useWs();
  return (
    <div className="min-h-full bg-neutral-50 dark:bg-neutral-950 text-neutral-900 dark:text-neutral-100">
      <header className="sticky top-0 z-10 flex items-center justify-between px-4 pb-3 pt-[calc(env(safe-area-inset-top)+12px)] bg-neutral-50/90 dark:bg-neutral-950/90 backdrop-blur-sm border-b border-neutral-200 dark:border-neutral-900">
        <h1 className="text-base font-bold">
          Projector <span className="text-orange-500">Remote</span>
        </h1>
        <ConnectionStatus connected={connected} />
      </header>

      <main className="px-4 pt-4 pb-48 max-w-md mx-auto flex flex-col gap-6">
        <FavoritesRow />

        <div className="flex gap-4 items-start">
          <div className="flex-1">
            <RemoteControls />
          </div>
          <VolumeSlider />
        </div>

        <TextEntry />
        <AppLauncher />
      </main>
    </div>
  );
}
