import { type ParentComponent, createMemo, createSignal, createEffect, onCleanup } from "solid-js";
import { useLocation } from "@solidjs/router";
import Sidebar from "./components/Sidebar";
import TopBar from "./components/TopBar";
import CommandPalette from "./components/CommandPalette";
import { sectionLabel } from "./lib/nav";
import { useTheme, applyTheme } from "./lib/theme";

// App is the authenticated shell: the nav rail, the sticky top bar, the routed
// page, and the global ⌘K command palette. It owns the rail collapse state, the
// theme effect (mirrors the mode onto <html>), and the ⌘K keybinding.
const App: ParentComponent = (props) => {
  const location = useLocation();
  const section = createMemo(() => sectionLabel(location.pathname));
  const theme = useTheme();

  const [collapsed, setCollapsed] = createSignal(localStorage.getItem("og-collapsed") === "1");
  const [paletteOpen, setPaletteOpen] = createSignal(false);

  createEffect(() => applyTheme(theme()));
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
        <main id="scroll-main" class="mx-auto w-full max-w-330 flex-1 overflow-y-auto px-8 pb-16 pt-7">
          {props.children}
        </main>
      </div>
      <CommandPalette open={paletteOpen()} onClose={() => setPaletteOpen(false)} />
    </div>
  );
};

export default App;
