import type { JSX } from "solid-js";
import { Show, createSignal, onCleanup, onMount } from "solid-js";
import { hostPlatform, shortcutModifier, shortcutHint } from "../lib/platform";
import { A } from "@solidjs/router";
import { For } from "solid-js";
import { collapseTrail, trailBudget, type Crumb } from "../lib/breadcrumb";
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

// A detail page's location path, the header's second line: every crumb a link,
// the middle ones folded into an ellipsis menu when the measured width cannot
// hold them (one line always, never wrapped).
function Trail(props: { crumbs: Crumb[] }) {
  const [width, setWidth] = createSignal(0);
  let el: HTMLElement | undefined;
  onMount(() => {
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setWidth(el!.clientWidth));
    ro.observe(el);
    setWidth(el.clientWidth);
    onCleanup(() => ro.disconnect());
  });
  const parts = () => collapseTrail(props.crumbs, trailBudget(width()));
  const sep = () => <span class="flex-none text-base-content/30">/</span>;
  return (
    <nav ref={el} aria-label="Breadcrumb" data-testid="breadcrumb" class="flex min-w-0 items-baseline gap-1.5 overflow-hidden whitespace-nowrap text-xs text-base-content/60">
      <For each={parts().head}>{(c, i) => <>{i() > 0 && sep()}<TrailLink crumb={c} /></>}</For>
      <Show when={parts().hidden.length > 0}>
        {sep()}
        <Popover class="btn btn-quiet btn-xs btn-square h-4 min-h-0 w-4 flex-none self-center" aria-label="Show hidden path" title="Show hidden path" placement="bottom-start" contentClass="menu z-50 w-56 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl focus:outline-none" content={
          <ul data-testid="trail-menu" class="flex flex-col">
            <For each={parts().hidden}>{(c) => <li><TrailLink crumb={c} /></li>}</For>
          </ul>
        }>
          <MoreHorizontal size={14} />
        </Popover>
      </Show>
      <For each={parts().tail}>{(c) => <>{sep()}<TrailLink crumb={c} /></>}</For>
    </nav>
  );
}

export default function TopBar(props: { section: string; title?: string | JSX.Element; hint?: string; trail?: Crumb[]; kind?: Crumb; system?: Crumb; onOpenPalette: () => void; onOpenOmnibot?: () => void }) {
  // Label the palette shortcut with the host-native modifier (⌘ on mac, Ctrl else).
  const mod = shortcutModifier(hostPlatform());
  const hint = shortcutHint(hostPlatform(), "K");
  return (
    <header class="navbar sticky top-0 z-20 min-h-14 gap-3 border-b border-base-300 bg-base-100/80 px-6 backdrop-blur">
      <div data-testid="page-header" class="flex min-w-0 flex-1 flex-col justify-center overflow-hidden py-1">
        <div data-testid="header-line-1" class="flex min-w-0 items-baseline gap-2 whitespace-nowrap">
          <Show when={props.kind} fallback={<Show when={props.section}>
            <span class="eyebrow flex-none text-base-content/70">{props.section.replaceAll(" · ", " / ")} /</span>
          </Show>}>
            {(k) => <span class="eyebrow flex flex-none items-baseline gap-2 text-base-content/70"><TrailLink crumb={k()} /><span class="text-base-content/30">/</span></span>}
          </Show>
          <h1 class="min-w-[4ch] shrink truncate text-lg font-semibold leading-tight tracking-tight">{props.title}</h1>
          <Show when={props.system}>
            {(sys) => <span class="min-w-0 shrink truncate text-sm text-base-content/60">(<TrailLink crumb={sys()} />)</span>}
          </Show>
        </div>
        <Show when={props.trail?.length} fallback={<Show when={props.hint}>
          <p data-testid="header-line-2" class="hidden truncate text-xs text-base-content/60 lg:block">{props.hint}</p>
        </Show>}>
          <div data-testid="header-line-2" class="min-w-0"><Trail crumbs={props.trail!} /></div>
        </Show>
      </div>
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
