import { type ParentComponent, createMemo, createSignal, createEffect, onCleanup } from "solid-js";
import { useLocation } from "@solidjs/router";
import Sidebar from "./components/Sidebar";
import TopBar from "./components/TopBar";
import ImpersonationBanner from "./components/ImpersonationBanner";
import CommandPalette from "./components/CommandPalette";
import { sectionLabel } from "./lib/nav";
import { useTheme, applyTheme, themeFromMe } from "./lib/theme";
import { useSettingsMe } from "./lib/settings";

// App is the authenticated shell: the nav rail, the sticky top bar, the routed
// page, and the global ⌘K command palette. It owns the rail collapse state, the
// theme effect (mirrors the mode onto <html>), and the ⌘K keybinding.
const App: ParentComponent = (props) => {
  const location = useLocation();
  const section = createMemo(() => sectionLabel(location.pathname));
  const theme = useTheme();
  // The effective theme comes from the settings engine (/settings/me). Until it
  // resolves (or for a value it does not carry), the dark-only default holds.
  const settingsMe = useSettingsMe();

  const [collapsed, setCollapsed] = createSignal(localStorage.getItem("og-collapsed") === "1");
  const [paletteOpen, setPaletteOpen] = createSignal(false);

  createEffect(() => {
    const me = settingsMe.data;
    applyTheme(me ? themeFromMe(me) : theme());
  });
  createEffect(() => localStorage.setItem("og-collapsed", collapsed() ? "1" : "0"));

  // Global ⌘K / Ctrl-K toggles the command palette.
  const onKey = (e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      setPaletteOpen((o) => !o);
    }
  };
  window.addEventListener("keydown", onKey);
  onCleanup(() => window.removeEventListener("keydown", onKey));

  return (
    <div class="flex min-h-screen bg-base-100">
      <Sidebar collapsed={collapsed()} onToggle={() => setCollapsed(!collapsed())} />
      <div class="flex min-w-0 flex-1 flex-col">
        <TopBar section={section()} onOpenPalette={() => setPaletteOpen(true)} />
        <ImpersonationBanner />
        <main id="scroll-main" class="mx-auto w-full max-w-330 flex-1 overflow-y-auto px-8 pb-16 pt-7">
          {props.children}
        </main>
      </div>
      <CommandPalette open={paletteOpen()} onClose={() => setPaletteOpen(false)} />
    </div>
  );
};

export default App;
