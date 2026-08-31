import { For, Show } from "solid-js";
import type { DotItem, FieldNode } from "../lib/explore_view";
import type { Verdict } from "../lib/health";

// The dot field (#839): a card's contents, drawn without naming a single
// level.
//
// The place tree has no fixed depth, so this spaces by SUBTREE HEIGHT rather
// than by what a level is called: a group's gap widens with how much is under
// it, which makes an uneven branch read as shallower instead of as broken.
// That is the whole trick that lets one renderer draw a campus of buildings
// and a two-level annex side by side.
//
// Hover and focus are handled by ONE delegated listener on the field, not by a
// listener per dot: at a thousand systems in a real estate that is the
// difference between a few handlers and a few thousand.

const VERDICT_CLASS: Record<string, string> = {
  healthy: "bg-success",
  incomplete: "bg-base-300",
  degraded: "bg-warning",
  outage: "bg-error",
};

function dotClass(v: Verdict | null): string {
  return VERDICT_CLASS[v ?? "healthy"] ?? "bg-base-300";
}

export type Density = "compact" | "cozy" | "roomy";

const DOT_PX: Record<Density, number> = { compact: 8, cozy: 11, roomy: 15 };
const GAP_PX: Record<Density, number> = { compact: 3, cozy: 5, roomy: 7 };

// A group's gap grows with the height of what it contains, capped so a deep
// tree does not push its siblings off the screen.
function gapFor(height: number, base: number): number {
  return Math.min(base * (1 + height * 1.8), base * 6);
}

function Dot(props: { item: DotItem; size: number; onPick?: (item: DotItem) => void }) {
  return (
    <button
      type="button"
      class={`rounded-full border-0 p-0 transition-transform hover:scale-150 focus-visible:scale-150 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary ${dotClass(props.item.verdict)}`}
      style={{ width: `${props.size}px`, height: `${props.size}px` }}
      title={props.item.label}
      aria-label={`${props.item.label}, ${props.item.verdict ?? "healthy"}`}
      data-dot={props.item.id}
      data-label={props.item.label}
      data-verdict={props.item.verdict ?? "healthy"}
      onClick={() => props.onPick?.(props.item)}
    />
  );
}

// One node's dots, with its name above them and a dashed enclosure around
// them. The label always sits ABOVE, left aligned, the way the section header
// sits above its cards: a node holding one thing drops the border and keeps
// the position, so the collapse changes the chrome and never the layout.
function Group(props: {
  node: FieldNode;
  density: Density;
  showLabels: boolean;
  showBoxes: boolean;
  onPick?: (item: DotItem) => void;
}) {
  const boxed = () => props.showBoxes && props.node.items.length > 1;
  const named = () => props.showLabels && props.node.label !== "" && props.node.items.length > 0;
  return (
    <div
      class="flex flex-wrap items-start"
      style={{ gap: `${gapFor(Math.max(0, props.node.height - 1), GAP_PX[props.density])}px` }}
    >
      <Show when={props.node.items.length > 0}>
        <div
          class="rounded"
          classList={{ "border border-dashed border-base-content/25 px-1.5 pb-1 pt-0.5": boxed(), "min-w-[4.5rem] pr-2": named() }}
        >
          <Show when={named()}>
            <span class="block max-w-[7rem] truncate font-mono text-[9px] leading-tight text-base-content/50" title={props.node.label}>
              {props.node.label}
              <Show when={props.node.items.length > 1}>{` · ${props.node.items.length}`}</Show>
            </span>
          </Show>
          <div class="flex flex-wrap pt-0.5" style={{ gap: `${GAP_PX[props.density]}px` }}>
            <For each={props.node.items}>
              {(item) => <Dot item={item} size={DOT_PX[props.density]} onPick={props.onPick} />}
            </For>
          </div>
        </div>
      </Show>
      <For each={props.node.children}>
        {(child) => (
          <Group node={child} density={props.density} showLabels={props.showLabels} showBoxes={props.showBoxes} onPick={props.onPick} />
        )}
      </For>
    </div>
  );
}

export default function DotField(props: {
  node: FieldNode;
  density: Density;
  showLabels?: boolean;
  showBoxes?: boolean;
  onHover?: (item: { id: string; label: string; verdict: string } | null) => void;
  onPick?: (item: DotItem) => void;
}) {
  // One listener for the whole field. Reading the identity off data
  // attributes keeps it independent of how deeply the groups nest.
  const describe = (e: Event) => {
    const el = (e.target as HTMLElement)?.closest?.("[data-dot]") as HTMLElement | null;
    if (!el) return;
    props.onHover?.({
      id: el.dataset.dot ?? "",
      label: el.dataset.label ?? "",
      verdict: el.dataset.verdict ?? "healthy",
    });
  };
  return (
    <div onMouseOver={describe} onFocusIn={describe} onMouseLeave={() => props.onHover?.(null)}>
      <Group node={props.node} density={props.density} showLabels={props.showLabels ?? false} showBoxes={props.showBoxes ?? false} onPick={props.onPick} />
    </div>
  );
}
