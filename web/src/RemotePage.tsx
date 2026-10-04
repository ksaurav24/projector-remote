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

      <main className="remote-layout px-4 pt-4 pb-48 max-w-md mx-auto flex flex-col gap-6">
        <div className="remote-intro hidden lg:block">
          <h2 className="text-3xl font-bold tracking-tight">Settle in.</h2>
          <p className="mt-2 text-sm text-neutral-500 dark:text-neutral-400">Everything you need to control the big screen.</p>
        </div>
        <section className="remote-favorites" aria-label="Favourite apps"><FavoritesRow /></section>
        <section className="remote-controller" aria-label="Projector controls">
          <h2 className="mb-5 hidden text-lg font-semibold lg:block">Screen controls</h2>
          <div className="flex gap-4 items-start">
            <div className="min-w-0 flex-1"><RemoteControls /></div>
            <VolumeSlider />
          </div>
          <p className="mt-6 hidden text-xs leading-relaxed text-neutral-500 lg:block">Use Tab to select a control, then Enter or Space to press it.</p>
        </section>
        <section className="remote-text" aria-label="Text entry">
          <TextEntry />
        </section>
        <section className="remote-apps" aria-label="Installed apps"><AppLauncher /></section>
      </main>
    </div>
  );
}
