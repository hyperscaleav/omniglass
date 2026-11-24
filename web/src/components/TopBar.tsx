import type { JSX } from "solid-js";
import { Show } from "solid-js";
import { hostPlatform, shortcutModifier, shortcutHint } from "../lib/platform";
import { Bot, Search } from "./icons";
import Button from "./Button";
import InboxIndicator from "./InboxIndicator";

// The sticky top bar (daisyUI navbar) is the page header, one row (#2575): the
// group trail and the page's one h1 title with its hint on the left, the
// command-palette trigger (a global jump, distinct from a page's own filter),
// omnibot's panel for a person who may run it (#1167), and the inbox bell with
// its unread count.
export default function TopBar(props: { section: string; title?: string | JSX.Element; hint?: string; onOpenPalette: () => void; onOpenOmnibot?: () => void }) {
  // Label the palette shortcut with the host-native modifier (⌘ on mac, Ctrl else).
  const mod = shortcutModifier(hostPlatform());
  const hint = shortcutHint(hostPlatform(), "K");
  return (
    <header class="navbar sticky top-0 z-20 min-h-14 gap-3 border-b border-base-300 bg-base-100/80 px-6 [&>*]:flex-none [&>div:first-child]:flex-initial backdrop-blur">
      <div data-testid="page-header" class="flex min-w-0 flex-col justify-center py-1">
        <div class="flex min-w-0 items-baseline gap-2 whitespace-nowrap">
          <Show when={props.section}>
            <span class="eyebrow flex-none text-base-content/70">{props.section.replaceAll(" · ", " / ")} /</span>
          </Show>
          <h1 class="truncate text-lg font-semibold leading-tight tracking-tight">{props.title}</h1>
        </div>
        <Show when={props.hint}>
          <p class="hidden truncate text-xs text-base-content/60 lg:block">{props.hint}</p>
        </Show>
      </div>
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
      <Button square icon={Search} onClick={props.onOpenPalette} title="Search and jump" label="Search and jump" class="sm:hidden" />
      <Show when={props.onOpenOmnibot}>
        {(open) => <Button square icon={Bot} onClick={open()} title="Ask omnibot" label="Ask omnibot" />}
      </Show>
      <InboxIndicator />
    </header>
  );
}
