import { useState } from "react";
import { AnimatePresence, MotionConfig } from "framer-motion";
import { WsProvider } from "./ws/WsContext";
import { OverlayProvider, useOverlays } from "./nav/Overlays";
import type { Overlay } from "./nav/Overlays";
import type { Ott } from "./catalog/types";
import { RemotePage } from "./RemotePage";
import { BrowsePage } from "./browse/BrowsePage";
import { SettingsPage } from "./settings/SettingsPage";
import { TitleSheet } from "./browse/TitleSheet";
import { SearchSheet } from "./browse/SearchSheet";
import { MiniBar } from "./playback/MiniBar";
import { NowPlayingSheet } from "./playback/NowPlayingSheet";
import { useWs } from "./ws/WsContext";
import { TabBar } from "./components/TabBar";
import type { Tab } from "./components/TabBar";

const TAB_KEY = "app.tab";

function storedTab(): Tab {
  try {
	const tab = localStorage.getItem(TAB_KEY);
	return tab === "browse" || tab === "settings" ? tab : "remote";
  } catch {
    return "remote";
  }
}

function OverlayView({ overlay }: { overlay: Overlay }) {
  const { open, close } = useOverlays();
  const openTitle = (ott: Ott, id: string) => open({ kind: "title", ott, id });
  switch (overlay.kind) {
    case "title":
      return <TitleSheet ott={overlay.ott} id={overlay.id} onClose={close} onOpenTitle={openTitle} />;
    case "search":
      return <SearchSheet ott={overlay.ott} onClose={close} onOpenTitle={openTitle} />;
    case "nowplaying":
      return <NowPlayingSheet onClose={close} />;
  }
}

function Shell() {
  const [tab, setTabState] = useState<Tab>(storedTab);
  const { stack, open } = useOverlays();
  const { nowPlaying } = useWs();

  function setTab(t: Tab) {
    setTabState(t);
    try {
      localStorage.setItem(TAB_KEY, t);
    } catch {
      // Not remembering the tab is fine.
    }
  }

  // Both pages stay mounted, each in its own scroll container, so
  // switching tabs keeps each one's scroll position and loaded data.
  return (
    <>
      <div hidden={tab !== "remote"} className="fixed inset-0 overflow-y-auto">
        <RemotePage />
      </div>
      <div hidden={tab !== "browse"} className="fixed inset-0 overflow-y-auto bg-black">
        <BrowsePage />
      </div>
      <div hidden={tab !== "settings"} className="fixed inset-0 overflow-y-auto">
        <SettingsPage />
      </div>
      <TabBar tab={tab} onChange={setTab} />
      <AnimatePresence>
        {nowPlaying && <MiniBar key="mini" np={nowPlaying} onExpand={() => open({ kind: "nowplaying" })} />}
      </AnimatePresence>
      <AnimatePresence>
        {stack.map((o, i) => (
          <OverlayView key={`${i}:${o.kind}:${"id" in o ? o.id : ""}`} overlay={o} />
        ))}
      </AnimatePresence>
    </>
  );
}

export default function App() {
  return (
    <MotionConfig reducedMotion="user">
      <WsProvider>
        <OverlayProvider>
          <Shell />
        </OverlayProvider>
      </WsProvider>
    </MotionConfig>
  );
}
