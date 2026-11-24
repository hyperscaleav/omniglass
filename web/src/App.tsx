import { type ParentComponent, Show, createMemo, createSignal, createEffect, onCleanup } from "solid-js";
import { useLocation } from "@solidjs/router";
import { pageHere, pendingTriage } from "./lib/handover";
import Sidebar from "./components/Sidebar";
import TopBar from "./components/TopBar";
import CommandPalette from "./components/CommandPalette";
import { SendCommandHost } from "./components/SendCommandButton";
import { Toaster } from "./components/ui/Toast";
import OmnibotPanel from "./components/OmnibotPanel";
import { can, useMe } from "./lib/auth";
import { sectionLabel } from "./lib/nav";
import { useTheme, applyTheme, themeFromMe } from "./lib/theme";
import { useSettingsMe } from "./lib/settings";

// App is the authenticated shell (the data-app-shell root, which an open blade
// makes inert; AppRoot gives it the viewport below the banner strips, and <main>
// is the scroll region): the nav rail, the top bar, the routed
// page, the global ⌘K command palette, omnibot's panel (#1167, for a person who
// may run it, from the top bar, the palette or an alarm's troubleshoot button), and the toasts any surface raises. It
// owns the rail collapse state, the theme effect (mirrors the mode onto
// <html>), and the ⌘K keybinding.
const App: ParentComponent = (props) => {
  const location = useLocation();
  const section = createMemo(() => sectionLabel(location.pathname));
  const theme = useTheme();
  // The effective theme comes from the settings engine (/settings/me). Until it
  // resolves (or for a value it does not carry), the dark-only default holds.
  const settingsMe = useSettingsMe();

  const [collapsed, setCollapsed] = createSignal(localStorage.getItem("og-collapsed") === "1");
  const [paletteOpen, setPaletteOpen] = createSignal(false);
  const [omnibotOpen, setOmnibotOpen] = createSignal(false);
  const [omnibotPrompt, setOmnibotPrompt] = createSignal<string>();
  const me = useMe();
  const mayAsk = () => can(me.data, "assistant", "use");
  const openOmnibot = (prompt?: string) => {
    setOmnibotPrompt(prompt || undefined);
    setOmnibotOpen(true);
  };

  // An alarm's troubleshoot button (#2164) asks for a triage; the panel runs
  // it once open.
  createEffect(() => {
    if (pendingTriage() && mayAsk()) setOmnibotOpen(true);
  });

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
    <>
    <div data-app-shell class="flex min-h-0 flex-1 bg-base-100">
      <Sidebar collapsed={collapsed()} onToggle={() => setCollapsed(!collapsed())} />
      <div class="flex min-w-0 flex-1 flex-col">
        <TopBar section={section()} onOpenPalette={() => setPaletteOpen(true)} onOpenOmnibot={mayAsk() ? () => openOmnibot() : undefined} />
        <main id="scroll-main" class="mx-auto w-full max-w-330 flex-1 overflow-y-auto px-8 pb-16 pt-7">
          {props.children}
        </main>
      </div>
    </div>
    <CommandPalette open={paletteOpen()} onClose={() => setPaletteOpen(false)} onAsk={openOmnibot} />
    <Show when={mayAsk()}>
      <OmnibotPanel open={omnibotOpen()} onClose={() => setOmnibotOpen(false)} prompt={omnibotPrompt()} page={pageHere(location.pathname, location.search)} />
    </Show>
    <SendCommandHost />
    <Toaster />
    </>
  );
};

export default App;
