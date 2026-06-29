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
    <div class="flex min-h-screen bg-base-100">
      <Sidebar collapsed={collapsed()} onToggle={() => setCollapsed(!collapsed())} />
      <div class="flex min-w-0 flex-1 flex-col">
        <TopBar section={section()} onOpenTweaks={() => setTweaksOpen(true)} />
        <main id="scroll-main" class="mx-auto w-full max-w-[1320px] flex-1 overflow-y-auto px-8 pb-16 pt-7">
          {props.children}
        </main>
      </div>
      <TweaksPanel open={tweaksOpen()} onClose={() => setTweaksOpen(false)} />
    </div>
  );
};

export default App;
