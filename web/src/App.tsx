import { WsProvider, useWs } from "./ws/WsContext";
import { ConnectionStatus } from "./components/ConnectionStatus";
import { Touchpad } from "./components/Touchpad";
import { ButtonGrid } from "./components/ButtonGrid";
import { TextEntry } from "./components/TextEntry";

function Shell() {
  const { connected } = useWs();
  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <ConnectionStatus connected={connected} />
      <main className="pt-10 px-4 max-w-md mx-auto pb-8">
        <h1 className="text-xl font-semibold text-center py-4">Projector Remote</h1>
        <Touchpad />
        <ButtonGrid />
        <TextEntry />
      </main>
    </div>
  );
}

export default function App() {
  return (
    <WsProvider>
      <Shell />
    </WsProvider>
  );
}
