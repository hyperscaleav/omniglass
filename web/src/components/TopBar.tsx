import { hostPlatform, shortcutHint } from "../lib/platform";
import { Search } from "./icons";
import Button from "./Button";

// The sticky top bar (daisyUI navbar): the current section label and the
// command-palette trigger (a global jump, distinct from a page's own filter). The
// shortcut label is driven by the keymap (passed as `paletteHint`), so a rebind in
// settings retitles the hint; it falls back to the native ⌘/Ctrl+K when absent.
export default function TopBar(props: { section: string; onOpenPalette: () => void; paletteHint?: string }) {
  const hint = () => props.paletteHint ?? shortcutHint(hostPlatform(), "K");
  return (
    <header class="navbar sticky top-0 z-20 min-h-14 gap-3 border-b border-base-300 bg-base-100/80 px-6 backdrop-blur">
      <span class="eyebrow text-base-content/70">{props.section}</span>
      <div class="flex-1" />
      <button
        class="hidden h-8 w-56 items-center gap-2 rounded-field border border-base-300 bg-base-200 px-3 text-sm text-base-content/40 sm:flex"
        onClick={props.onOpenPalette}
        title={`Search and jump (${hint()})`}
      >
        <Search size={15} />
        <span>Search</span>
        <span class="ml-auto flex items-center gap-1">
          <kbd class="kbd kbd-sm leading-none">{hint()}</kbd>
        </span>
      </button>
      <Button square icon={Search} onClick={props.onOpenPalette} title="Search and jump" label="Search and jump" class="sm:hidden" />
    </header>
  );
}
