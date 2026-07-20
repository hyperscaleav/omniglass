import { type ParentComponent, createMemo, createSignal, createEffect, onCleanup, onMount } from "solid-js";
import { useLocation } from "@solidjs/router";
import Sidebar from "./components/Sidebar";
import TopBar from "./components/TopBar";
import ImpersonationBanner from "./components/ImpersonationBanner";
import CommandPalette from "./components/CommandPalette";
import KeyboardHelp from "./components/KeyboardHelp";
import { KeymapProvider, useKeymap } from "./components/KeymapProvider";
import { sectionLabel } from "./lib/nav";
import { useTheme, applyTheme, themeFromMe } from "./lib/theme";
import { useSettingsMe, type SettingsMe } from "./lib/settings";
import { keybindingsFromMe } from "./lib/keymap";
import { formatCombo } from "./lib/platform";

// App is the authenticated shell. It owns the settings read (theme + keymap) and
// wraps the shell in the KeymapProvider so the whole console shares one shortcut
// registry (epic #303). The keymap is the effective `keybindings` namespace from
// /settings/me layered over the code defaults, so an operator override re-binds a key
// with no code change.
const App: ParentComponent = (props) => {
  const settingsMe = useSettingsMe();
  const keys = createMemo(() => keybindingsFromMe(settingsMe.data));
  return (
    <KeymapProvider keys={keys}>
      <Shell me={() => settingsMe.data}>{props.children}</Shell>
    </KeymapProvider>
  );
};

// Shell is the rail + top bar + routed page, inside the provider so it can register
// the global keyboard scope (the command palette and the help overlay) and open both.
const Shell: ParentComponent<{ me: () => SettingsMe | undefined }> = (props) => {
  const location = useLocation();
  const section = createMemo(() => sectionLabel(location.pathname));
  const theme = useTheme();
  const km = useKeymap();

  const [collapsed, setCollapsed] = createSignal(localStorage.getItem("og-collapsed") === "1");
  const [paletteOpen, setPaletteOpen] = createSignal(false);
  const [helpOpen, setHelpOpen] = createSignal(false);

  // The effective theme comes from the settings engine (/settings/me). Until it
  // resolves (or for a value it does not carry), the dark-only default holds.
  createEffect(() => {
    const me = props.me();
    applyTheme(me ? themeFromMe(me) : theme());
  });
  createEffect(() => localStorage.setItem("og-collapsed", collapsed() ? "1" : "0"));

  // The global scope: the command palette (from settings, mod+k by default) and the
  // help overlay (?). Both toggle their dialogs; the registry owns the keys.
  onMount(() => {
    const off = km.register({
      name: "global",
      priority: 10,
      bindings: () => [
        { action: "command_palette", label: "Command palette", combo: km.keys().command_palette, run: () => setPaletteOpen((o) => !o) },
        { action: "help", label: "Keyboard shortcuts", combo: "?", run: () => setHelpOpen((o) => !o) },
      ],
    });
    onCleanup(off);
  });

  const paletteHint = () => formatCombo(km.platform(), km.keys().command_palette);

  return (
    <div class="flex min-h-screen bg-base-100">
      <Sidebar collapsed={collapsed()} onToggle={() => setCollapsed(!collapsed())} />
      <div class="flex min-w-0 flex-1 flex-col">
        <TopBar section={section()} onOpenPalette={() => setPaletteOpen(true)} paletteHint={paletteHint()} />
        <ImpersonationBanner />
        <main id="scroll-main" class="mx-auto w-full max-w-330 flex-1 overflow-y-auto px-8 pb-16 pt-7">
          {props.children}
        </main>
      </div>
      <CommandPalette open={paletteOpen()} onClose={() => setPaletteOpen(false)} onShowHelp={() => setHelpOpen(true)} />
      <KeyboardHelp open={helpOpen()} onClose={() => setHelpOpen(false)} />
    </div>
  );
};

export default App;
