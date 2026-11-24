import type { JSX } from "solid-js";
import { Show } from "solid-js";
import { hostPlatform, shortcutModifier, shortcutHint } from "../lib/platform";
import { A } from "@solidjs/router";
import { For } from "solid-js";
import { collapseTrail, type Crumb } from "../lib/breadcrumb";
import { Bot, MoreHorizontal, Search } from "./icons";
import Popover from "./ui/Popover";
import Button from "./Button";
import InboxIndicator from "./InboxIndicator";

// The sticky top bar (daisyUI navbar) is the page header, one row (#2575): the
// group trail and the page's one h1 title with its hint on the left, the
// command-palette trigger (a global jump, distinct from a page's own filter),
// omnibot's panel for a person who may run it (#1167), and the inbox bell with
// its unread count.
function TrailLink(props: { crumb: Crumb }) {
  return (
    <Show when={props.crumb.href} fallback={<span title={props.crumb.title ?? props.crumb.label}>{props.crumb.label}</span>}>
      {(href) => (
        <A href={href()} title={props.crumb.title ?? props.crumb.label} class="hover:text-base-content">
          {props.crumb.label}
        </A>
      )}
    </Show>
  );
}

// A detail page's trail: every crumb a link, the middle ones folded into an
// ellipsis menu when the path would crowd the title and the controls.
function Trail(props: { crumbs: Crumb[] }) {
  const parts = () => collapseTrail(props.crumbs);
  const sep = () => <span class="flex-none text-base-content/30">/</span>;
  return (
    <nav aria-label="Breadcrumb" data-testid="breadcrumb" class="eyebrow flex min-w-0 flex-none items-baseline gap-1.5 text-base-content/70">
      <For each={parts().head}>{(c) => <><TrailLink crumb={c} />{sep()}</>}</For>
      <Show when={parts().hidden.length > 0}>
        <Popover class="btn btn-quiet btn-xs btn-square self-center" aria-label="Show hidden path" title="Show hidden path" placement="bottom-start" contentClass="menu z-50 w-56 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl focus:outline-none" content={
          <ul data-testid="trail-menu" class="flex flex-col">
            <For each={parts().hidden}>{(c) => <li><TrailLink crumb={c} /></li>}</For>
          </ul>
        }>
          <MoreHorizontal size={14} />
        </Popover>
        {sep()}
      </Show>
      <For each={parts().tail}>{(c) => <><TrailLink crumb={c} />{sep()}</>}</For>
    </nav>
  );
}

export default function TopBar(props: { section: string; title?: string | JSX.Element; hint?: string; trail?: Crumb[]; onOpenPalette: () => void; onOpenOmnibot?: () => void }) {
  // Label the palette shortcut with the host-native modifier (⌘ on mac, Ctrl else).
  const mod = shortcutModifier(hostPlatform());
  const hint = shortcutHint(hostPlatform(), "K");
  return (
    <header class="navbar sticky top-0 z-20 min-h-14 gap-3 border-b border-base-300 bg-base-100/80 px-6 backdrop-blur">
      <div data-testid="page-header" class="flex min-w-0 flex-col justify-center py-1">
        <div class="flex min-w-0 items-baseline gap-2 whitespace-nowrap">
          <Show when={props.trail?.length} fallback={<Show when={props.section}>
            <span class="eyebrow flex-none text-base-content/70">{props.section.replaceAll(" · ", " / ")} /</span>
          </Show>}>
            <Trail crumbs={props.trail!} />
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
