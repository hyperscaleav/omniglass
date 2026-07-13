import { For, Show, createSignal, onCleanup, type JSX } from "solid-js";
import { Tooltip } from "@kobalte/core/tooltip";
import { tagHue } from "../lib/tagcolor";

// TagPills renders an entity's effective tags as colored key=value chips, one per
// key, sorted for a stable order. Each chip's color is derived from its key
// (tagHue), crossing only the hue into the .tag-pill CSS recipe as --tag-h; the
// recipe and the per-theme lightness/chroma live in the stylesheet.
//
// Two layouts. The default (a directory column) keeps the chips on ONE line,
// clipping the overflow with a right-edge fade, and reveals the full set in a
// portaled tooltip on hover, so a dense row stays scannable. Passing `wrap` lays
// every chip out on as many rows as it takes (a detail blade, and the seam for a
// future per-table wrap toggle).
export default function TagPills(props: { tags?: Record<string, string>; wrap?: boolean }): JSX.Element {
  const keys = () => Object.keys(props.tags ?? {}).sort();
  const chips = () => (
    <For each={keys()}>
      {(k) => (
        <span class="badge badge-sm tag-pill shrink-0" style={{ "--tag-h": String(tagHue(k)) }}>
          <span class="font-medium">{k}</span>
          <span class="opacity-40">=</span>
          <span>{props.tags![k]}</span>
        </span>
      )}
    </For>
  );

  return (
    <Show when={keys().length} fallback={<span class="text-base-content/40">—</span>}>
      <Show
        when={!props.wrap}
        fallback={<span class="inline-flex flex-wrap items-center gap-1">{chips()}</span>}
      >
        <OneLine full={() => <span class="inline-flex flex-wrap items-center gap-1">{chips()}</span>}>
          {chips()}
        </OneLine>
      </Show>
    </Show>
  );
}

// OneLine clips its children to a single row (fading the right edge only when
// they actually overflow) and shows `full` in a hover tooltip. The tooltip is
// portaled so the table's own overflow never clips it, and it is suppressed when
// nothing is hidden (a short row that fits reveals nothing new). The overflow
// probe is one container-level measurement, re-run on resize.
function OneLine(props: { children: JSX.Element; full: () => JSX.Element }): JSX.Element {
  const [overflowing, setOverflowing] = createSignal(false);
  const [open, setOpen] = createSignal(false);

  const measure = (el: HTMLElement) => setOverflowing(el.scrollWidth > el.clientWidth + 1);
  const attach = (el: HTMLSpanElement) => {
    // jsdom (the test env) has no ResizeObserver and zero layout, so guard it;
    // there the row never reads as overflowing, which is the correct default.
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => measure(el));
    ro.observe(el);
    onCleanup(() => ro.disconnect());
  };

  return (
    <Tooltip open={open() && overflowing()} onOpenChange={setOpen} openDelay={200} closeDelay={100} placement="top-start" gutter={4}>
      <Tooltip.Trigger as="span" class="flex w-full min-w-0 cursor-default align-middle">
        <span ref={attach} class="flex w-full min-w-0 flex-nowrap items-center gap-1 overflow-hidden" classList={{ "tag-fade": overflowing() }}>
          {props.children}
        </span>
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content class="z-100 max-w-sm rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
          {props.full()}
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip>
  );
}
