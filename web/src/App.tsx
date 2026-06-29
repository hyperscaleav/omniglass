import { type ParentComponent, createMemo, createSignal, createEffect } from "solid-js";
import { useLocation } from "@solidjs/router";
import Sidebar from "./components/Sidebar";
import TopBar from "./components/TopBar";
import TweaksPanel from "./components/Tweaks";
import { sectionLabel } from "./lib/nav";
import { useTweaks, applyTweaks } from "./lib/tweaks";

// App is the authenticated shell: the nav rail, the sticky top bar, and the
// routed page, with the Tweaks slide-over. It owns the collapse state and the
// effect that mirrors the display tweaks onto <html>.
const App: ParentComponent = (props) => {
  const location = useLocation();
  const section = createMemo(() => sectionLabel(location.pathname));
  const tweaks = useTweaks();

  const [collapsed, setCollapsed] = createSignal(localStorage.getItem("og-collapsed") === "1");
  const [tweaksOpen, setTweaksOpen] = createSignal(false);

  createEffect(() => applyTweaks(tweaks()));
  createEffect(() => localStorage.setItem("og-collapsed", collapsed() ? "1" : "0"));

  return (
    <div style={{ display: "flex", "min-height": "100vh", background: "var(--ground)" }}>
      <Sidebar collapsed={collapsed()} onToggle={() => setCollapsed(!collapsed())} />
      <div style={{ flex: 1, "min-width": 0, display: "flex", "flex-direction": "column" }}>
        <TopBar section={section()} onOpenTweaks={() => setTweaksOpen(true)} />
        <main id="scroll-main" style={{ flex: 1, "overflow-y": "auto", padding: "28px 32px 64px", "max-width": "1320px", width: "100%", margin: "0 auto" }}>
          {props.children}
        </main>
      </div>
      <TweaksPanel open={tweaksOpen()} onClose={() => setTweaksOpen(false)} />
    </div>
  );
};

export default App;
