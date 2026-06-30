import { Show } from "solid-js";
import { useTheme, toggleTheme } from "../lib/theme";
import { hostPlatform, shortcutModifier, shortcutHint } from "../lib/platform";
import { Sun, Moon, Search } from "./icons";

// The sticky top bar (daisyUI navbar): the current section label, the
// command-palette trigger (a global jump, distinct from a page's own filter),
// and the dark/light theme toggle.
export default function TopBar(props: { section: string; onOpenPalette: () => void }) {
  const theme = useTheme();
  // Label the palette shortcut with the host-native modifier (⌘ on mac, Ctrl else).
  const mod = shortcutModifier(hostPlatform());
  const hint = shortcutHint(hostPlatform(), "K");
  return (
    <header class="navbar sticky top-0 z-20 min-h-14 gap-3 border-b border-base-300 bg-base-100/80 px-6 backdrop-blur">
      <span class="eyebrow text-base-content/70">{props.section}</span>
      <div class="flex-1" />
      <button
        class="hidden h-8 w-56 items-center gap-2 rounded-field border border-base-300 bg-base-200 px-3 text-sm text-base-content/40 sm:flex"
        onClick={props.onOpenPalette}
        title={`Search and jump (${hint})`}
      >
        <Search size={15} />
        <span>Search</span>
        <span class="ml-auto flex items-center gap-1">
          <kbd class="kbd kbd-sm leading-none">{mod}</kbd>
          <kbd class="kbd kbd-sm leading-none">K</kbd>
        </span>
      </button>
      <button class="btn btn-ghost btn-sm btn-square sm:hidden" onClick={props.onOpenPalette} title="Search and jump">
        <Search size={17} />
      </button>
      <button class="btn btn-ghost btn-sm btn-square" title={theme() === "dark" ? "Light mode" : "Dark mode"} onClick={toggleTheme}>
        <Show when={theme() === "dark"} fallback={<Moon size={16} />}><Sun size={16} /></Show>
      </button>
    </header>
  );
}
