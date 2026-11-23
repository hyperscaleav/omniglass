import { createMemo, createSignal, For, Show, type JSX } from "solid-js";
import { Dynamic } from "solid-js/web";
import { Popover } from "@kobalte/core/popover";
import { ChevronRight, CircleCheck, CircleDashed, LayoutDashboard, OctagonX, Plus, TriangleAlert, resolveIcon } from "./icons";
import type { ComponentGroup, ComponentRow, Lights, LocationNode } from "../lib/outline";
import { NO_COMPONENTS } from "../lib/outline";
import { HEALTH_HUE, type HealthState } from "../lib/health";
import OutlineName from "./OutlineName";
import Tooltip from "./ui/Tooltip";

// The outline (#867): every location in the fleet as one tree, drawn as aligned
// columns. A location wears its system (standard and health on its own row) and
// lists the components beneath it directly; only a location holding several
// systems shows a header per system. Health is four fixed slots, and only
// healthy wears a muted green beside the full trouble hues, so a healthy fleet
// reads calm and the one degraded branch is the loudest colour on the page.
//
// The geometry is one grid shared by every row, measured against the
// wireframe: text starts at one x per depth whatever the row's kind, and each
// indent guide sits under its parent's chevron.

export type AddKind = "location" | "system" | "component";
export type OpenTarget = { kind: "location" | "system" | "component"; id: string };

// One grid for every row and the header: name, type, standard or product,
// detail, health, action. The columns answer the width of the outline itself
// (a container query), not the window, since the sidebar and the blade take
// their share first: as it narrows, Standard or product goes, then Type
// (Detail slims), then Detail, and at the narrowest Health drops its empty
// slots; Name keeps what is left. OUTLINE_FRAME marks the container.
export const OUTLINE_FRAME = "@container";
export const OUTLINE_COLS =
  "grid-cols-[minmax(0,1fr)_7rem_12rem_10rem_9.5rem_2rem] @max-[56rem]:grid-cols-[minmax(0,1fr)_7rem_10rem_9.5rem_2rem] @max-[40rem]:grid-cols-[minmax(0,1fr)_7rem_9.5rem_2rem] @max-[28rem]:grid-cols-[minmax(0,1fr)_6rem_2rem]";
export const WIDE_ONLY = "@max-[56rem]:hidden";
export const ROOMY_ONLY = "@max-[40rem]:hidden";
export const NARROW_HIDDEN = "@max-[28rem]:hidden";
const PADX = 16;
const CHEV = 16;
// One level of nesting, as a custom property the narrowest tier halves, so a
// deep row keeps room for its name beside an open sidebar.
const INDENT_VARS = "[--og-indent:22px] @max-[28rem]:[--og-indent:12px]";

// The expander's accessible name. Same-named places under different parents get
// the parent path appended; the visible row text is unchanged.
export const expanderName = (open: boolean, label: string, within: string) =>
  `${open ? "Collapse" : "Expand"} ${label}${within ? ` in ${within}` : ""}`;

export type Row =
  | { kind: "location"; key: string; depth: number; node: LocationNode; parent: string | null; within: string }
  | { kind: "group"; key: string; depth: number; group: ComponentGroup; location: LocationNode; parent: string }
  | { kind: "component"; key: string; depth: number; row: ComponentRow; location: LocationNode; parent: string };

const HEALTHS: HealthState[] = ["healthy", "incomplete", "degraded", "outage"];
const WORDS: Record<HealthState, string> = { healthy: "healthy", incomplete: "incomplete", degraded: "degraded", outage: "outage" };
// Healthy wears the muted green (#1067), so the one troubled branch still pops.
const HUE = HEALTH_HUE;
const GLYPH: Record<HealthState, typeof CircleCheck> = { healthy: CircleCheck, incomplete: CircleDashed, degraded: TriangleAlert, outage: OctagonX };

// One health as its glyph alone, in the slot vocabulary's hue (healthy
// muted green), named for a screen reader: where a row is one thing, not a count.
export function HealthGlyph(props: { health: HealthState | null }) {
  return (
    <Show when={props.health}>
      {(v) => (
        <span data-testid="health" class={`inline-flex ${HUE[v()]}`} role="img" aria-label={WORDS[v()]} title={WORDS[v()]}>
          <Dynamic component={GLYPH[v()]} size={14} />
        </span>
      )}
    </Show>
  );
}

// Health counts pack left in severity order, each with its count: a row shows
// only the healths it has, so a lone warning sits first rather than in a third
// slot after a blank. `single` is one thing's own verdict (a component), which
// is a glyph and no count; `lights` is a place's systems by health.
export function HealthSlots(props: { lights?: Lights; single?: HealthState | null }) {
  const counts = (): Partial<Record<HealthState, number>> => {
    if (props.single) return { [props.single]: 0 };
    const l = props.lights;
    return l ? { healthy: l.healthy, incomplete: l.incomplete, degraded: l.degraded, outage: l.outage } : {};
  };
  const shown = () => HEALTHS.filter((v) => (props.single ? props.single === v : (counts()[v] ?? 0) > 0));
  const words = () => {
    if (props.single) return WORDS[props.single];
    return shown().map((v) => `${counts()[v]} ${WORDS[v]}`).join(", ");
  };
  return (
    <span data-testid="health" class="flex items-center gap-2.5 font-data text-[12.5px] font-medium" aria-label={words() || undefined} title={words() || undefined}>
      <For each={shown()}>
        {(v) => {
          const Glyph = GLYPH[v];
          return (
            <span data-slot={v} class={`inline-flex items-center gap-1 ${HUE[v]}`} aria-hidden="true">
              <Glyph size={14} />
              <Show when={!props.single}>{counts()[v]}</Show>
            </span>
          );
        }}
      </For>
    </span>
  );
}

// A Detail longer than this wraps to its second line and may still be cut, so
// it carries a tooltip with the whole of it; a short one needs neither.
const DETAIL_TIP_AT = 28;

// The Detail cell's text: up to two lines, and the whole text on hover or
// focus when it is long enough to be cut.
export function DetailText(props: { text: string }) {
  return (
    <Show when={props.text.length > DETAIL_TIP_AT} fallback={<span>{props.text}</span>}>
      <Tooltip as="span" content={props.text} tabIndex={-1} class="line-clamp-2 cursor-default text-left">
        {props.text}
      </Tooltip>
    </Show>
  );
}

export default function Outline(props: {
  roots: LocationNode[];
  expanded: Set<string>;
  selected: string | null;
  onToggle: (node: LocationNode, deep: boolean) => void;
  onOpen: (target: OpenTarget, key: string) => void;
  addOptions: (location: LocationNode["chain"][number]) => AddKind[];
  onAdd: (locationId: string, kind: AddKind) => void;
  // The active alarm behind a component's light, when the caller has read one.
  issueOf?: (componentId: string) => string | undefined;
}) {
  // What the row's + offers: a section per location the row stands for. A folded
  // row is several locations, and the outer ones must stay reachable, or a campus
  // of one building could never gain a second.
  const addTargets = (n: LocationNode): AddTarget[] =>
    n.id === "unplaced" ? [] : n.chain.map((c) => ({ id: c.id, label: c.label, kinds: props.addOptions(c) })).filter((t) => t.kinds.length > 0);
  // A header per system only where a location holds several; the node of
  // systems placed out of sight exists to list them, so it always heads.
  const grouped = (n: LocationNode) => n.id === "unplaced" || n.groups.filter((g) => g.system).length > 1;
  const rows = createMemo<Row[]>(() => {
    const out: Row[] = [];
    const walk = (n: LocationNode, depth: number, parent: string | null, within: string) => {
      out.push({ kind: "location", key: n.id, depth, node: n, parent, within });
      const path = [within, ...n.chain.map((c) => c.label)].filter(Boolean).join(" / ");
      if (!props.expanded.has(n.id)) return;
      for (const c of n.children) walk(c, depth + 1, n.id, path);
      for (const g of n.groups) {
        const headed = grouped(n) && g.system !== null;
        if (headed) out.push({ kind: "group", key: `${n.id}:${g.system!.id}`, depth: depth + 1, group: g, location: n, parent: n.id });
        for (const c of g.components) {
          out.push({ kind: "component", key: `${n.id}:${g.system?.id ?? "-"}:${c.id}`, depth: depth + (headed ? 2 : 1), row: c, location: n, parent: headed ? `${n.id}:${g.system!.id}` : n.id });
        }
      }
    };
    for (const r of props.roots) walk(r, 0, null, "");
    return out;
  });
  // The rows are rebuilt whenever anything changes (a toggle, a health read),
  // so <For> runs over their keys, which are strings and so equal across
  // rebuilds: a row's element, its focus and an open menu all survive.
  const keys = createMemo(() => rows().map((r) => r.key), undefined, { equals: (a, b) => a.length === b.length && a.every((k, i) => k === b[i]) });
  const byKey = createMemo(() => new Map(rows().map((r, i) => [r.key, { row: r, index: i }] as const)));

  const [focusKey, setFocusKey] = createSignal<string | null>(null);
  const tabKey = () => {
    const k = focusKey();
    return k && rows().some((r) => r.key === k) ? k : rows()[0]?.key ?? null;
  };
  let host!: HTMLDivElement;
  const focusRow = (key: string) => {
    setFocusKey(key);
    host?.querySelector<HTMLElement>(`[data-key="${CSS.escape(key)}"]`)?.focus();
  };

  const targetOf = (r: Row): OpenTarget | null => {
    if (r.kind === "component") return { kind: "component", id: r.row.id };
    if (r.kind === "group") return { kind: "system", id: r.group.system!.id };
    if (r.node.id === "unplaced") return null;
    return r.node.systems.length === 1 ? { kind: "system", id: r.node.systems[0].id } : { kind: "location", id: r.node.id };
  };
  // Only a row that would open onto something wears a chevron: a location whose
  // one system holds no components yet has nothing beneath it.
  const expandable = (r: Row) =>
    r.kind === "location" && (r.node.children.length > 0 || r.node.groups.some((g) => g.components.length > 0 || (grouped(r.node) && g.system !== null)));
  // What a selection names: a location row answers to every location a fold joined
  // into it, and to the system it wears; a header or a component to its own.
  const answersTo = (r: Row, id: string) =>
    r.kind === "location"
      ? r.node.chain.some((c) => c.id === id) || (r.node.id !== "unplaced" && r.node.systems.length === 1 && r.node.systems[0].id === id)
      : (r.kind === "component" ? r.row.id : r.group.system!.id) === id;

  const onKey = (e: KeyboardEvent, r: Row, i: number) => {
    // The add menu is portaled out of the row but its events still bubble
    // through it (Solid re-parents a portal's delegated events): a key meant
    // for the menu is not the row's.
    if ((e.target as Element).closest?.("[role=treeitem]") !== e.currentTarget) return;
    const list = rows();
    if (e.key === "ArrowDown" && i + 1 < list.length) { e.preventDefault(); focusRow(list[i + 1].key); }
    else if (e.key === "ArrowUp" && i > 0) { e.preventDefault(); focusRow(list[i - 1].key); }
    else if (e.key === "ArrowRight" && r.kind === "location" && expandable(r)) {
      e.preventDefault();
      if (!props.expanded.has(r.node.id)) props.onToggle(r.node, false);
      else if (list[i + 1]) focusRow(list[i + 1].key);
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      if (r.kind === "location" && props.expanded.has(r.node.id)) props.onToggle(r.node, false);
      else if (r.parent) focusRow(r.parent);
    } else if (e.key === "*" && r.kind === "location") { e.preventDefault(); props.onToggle(r.node, true); }
    else if (e.key === "+") {
      const add = (e.currentTarget as HTMLElement).querySelector<HTMLElement>("[data-add]");
      if (add) { e.preventDefault(); add.click(); }
    }
    else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      const t = targetOf(r);
      if (t) props.onOpen(t, r.key);
      else if (r.kind === "location" && expandable(r)) props.onToggle(r.node, false);
    }
  };

  const guides = (depth: number): JSX.CSSProperties => ({
    "background-image": `repeating-linear-gradient(to right, color-mix(in oklch, var(--color-base-content) 9%, transparent) 0 1px, transparent 1px var(--og-indent))`,
    "background-size": `calc(${depth} * var(--og-indent)) 100%`,
    "background-repeat": "no-repeat",
    "background-position": `${PADX + CHEV / 2}px 0`,
  });

  return (
    <div ref={host} role="tree" aria-label="Locations" class={`${OUTLINE_FRAME} pb-2`}>
      <div class={`grid ${OUTLINE_COLS} h-8 items-center border-b border-base-300 px-4 text-xs font-medium text-base-content/40`} aria-hidden="true">
        <span style={{ "padding-left": `${CHEV + 8 + 16 + 8}px` }}>Name</span>
        <span class={ROOMY_ONLY}>Type</span>
        <span class={WIDE_ONLY}>Standard or product</span>
        <span class={NARROW_HIDDEN}>Detail</span>
        <span>Health</span>
        <span />
      </div>
      <For each={keys()}>
        {(key) => {
          // A key is removed before its row is, so hold the last row seen.
          let last!: { row: Row; index: number };
          const at = () => (last = byKey().get(key) ?? last);
          const r = () => at().row;
          const i = () => at().index;
          const node = () => { const x = r(); return x.kind === "location" ? x.node : undefined; };
          const comp = () => { const x = r(); return x.kind === "component" ? x.row : undefined; };
          const grp = () => { const x = r(); return x.kind === "group" ? x.group : undefined; };
          const issue = () => { const x = r(); return x.kind === "component" ? props.issueOf?.(x.row.id) : undefined; };
          const isOpen = () => r().kind === "location" && props.expanded.has(node()!.id);
          const isSel = () => props.selected !== null && answersTo(r(), props.selected);
          return (
            <div
              role="treeitem"
              data-key={key}
              aria-level={r().depth + 1}
              aria-expanded={expandable(r()) ? (isOpen() ? "true" : "false") : undefined}
              aria-selected={isSel() ? "true" : "false"}
              tabindex={tabKey() === key ? 0 : -1}
              class={`group/row relative grid ${OUTLINE_COLS} ${INDENT_VARS} h-9 cursor-pointer items-center px-4 outline-none hover:bg-base-content/[0.03] focus-visible:bg-base-content/[0.05] focus-visible:shadow-[inset_0_0_0_1px_var(--color-primary)]`}
              classList={{ "bg-primary/[0.07] shadow-[inset_2px_0_0_var(--color-primary)]": isSel() }}
              style={guides(r().depth)}
              onFocus={() => setFocusKey(key)}
              onKeyDown={(e) => onKey(e, r(), i())}
              onClick={() => {
                setFocusKey(key);
                const t = targetOf(r());
                if (t) props.onOpen(t, r().key);
                else if (expandable(r())) props.onToggle((r() as { node: LocationNode }).node, false);
              }}
            >
              <span class="flex min-w-0 items-center gap-2 pr-4" style={{ "padding-left": `calc(${r().depth} * var(--og-indent))` }}>
                <Show
                  when={r().kind === "location" && expandable(r())}
                  fallback={<span class="w-4 flex-none" aria-hidden="true" />}
                >
                  <button
                    type="button"
                    tabindex={-1}
                    class="grid h-4 w-4 flex-none cursor-pointer place-items-center rounded text-base-content/50 hover:text-base-content"
                    aria-label={expanderName(isOpen(), node()!.chain[node()!.chain.length - 1].label, (r() as { within: string }).within)}
                    onClick={(e) => { e.stopPropagation(); if (r().kind === "location") props.onToggle(node()!, e.altKey); }}
                  >
                    <span class="transition-transform" classList={{ "rotate-90": isOpen() }}><ChevronRight size={12} /></span>
                  </button>
                </Show>
                <Show when={r().kind === "location"}>
                  {(() => {
                    const n = () => (r() as { node: LocationNode }).node;
                    return (
                      <>
                        <span class="flex-none text-base-content/50"><Dynamic component={resolveIcon(n().icon || "map-pin")} size={16} /></span>
                        {/* A folded row's outer path gives way first: the location's own
                            name is what the row is, so it keeps its width (capped at
                            the cell) and truncates only once the path is down to its
                            ellipsis. At the narrowest the path steps aside entirely,
                            and the whole of it stays on hover. */}
                        <span class="flex min-w-0 items-baseline" title={n().chain.map((c) => c.label).join(" / ")}>
                          <Show when={n().chain.length > 1}>
                            <span class="min-w-[1.25em] shrink-[999] truncate font-medium text-base-content/55 @max-[28rem]:hidden">
                              <For each={n().chain.slice(0, -1)}>
                                {(c, i) => <>{i() > 0 && <span class="mx-1.5 text-base-content/30">/</span>}{c.label}</>}
                              </For>
                            </span>
                            <span class="mx-1.5 shrink-0 text-base-content/30 @max-[28rem]:hidden">/</span>
                          </Show>
                          <OutlineName kind="location" label={n().chain[n().chain.length - 1].label} class="min-w-0 max-w-full shrink-0" />
                        </span>
                      </>
                    );
                  })()}
                </Show>
                <Show when={r().kind === "group"}>
                  <span class="flex-none text-base-content/35"><LayoutDashboard size={16} /></span>
                  <OutlineName kind="group" label={(r() as { group: ComponentGroup }).group.system!.label} />
                </Show>
                <Show when={r().kind === "component"}>
                  {(() => {
                    const c = () => (r() as { row: ComponentRow }).row;
                    return (
                      <>
                        <span class="flex-none text-base-content/35"><Dynamic component={resolveIcon(c().icon)} size={16} /></span>
                        <OutlineName kind="component" label={c().label} class="min-w-[4rem]" />
                        {/* The chip gives way first, so the name stays readable beside it: it
                            shrinks before the name does, and in a narrow outline it is a bare
                            count whose title carries the systems. */}
                        <Show when={c().also.length > 0}>
                          <span class="shrink-[999] truncate rounded border border-base-content/15 px-1.5 text-[11px] leading-4 text-base-content/55 @max-[56rem]:px-1" title={`Also in ${c().also.join(", ")}`}>
                            <span class="@max-[56rem]:hidden">also in {c().also.join(", ")}</span>
                            <span class="hidden @max-[56rem]:inline">+{c().also.length}</span>
                          </span>
                        </Show>
                      </>
                    );
                  })()}
                </Show>
              </span>
              <span class={`truncate pr-4 text-base-content/55 ${ROOMY_ONLY}`}>{r().kind === "location" ? node()!.type : ""}</span>
              <span data-testid={r().kind === "location" && node()!.systems.length === 1 ? "standard" : undefined} class={`truncate pr-4 text-base-content/75 ${WIDE_ONLY}`}>
                {r().kind === "location"
                  ? (node()!.systems.length === 1 ? node()!.systems[0].standard : "")
                  : r().kind === "component" ? comp()!.product : grp()!.system!.standard}
              </span>
              <span
                class={`pr-4 text-[13px] leading-4 ${NARROW_HIDDEN}`}
                classList={{ "text-error": !!issue(), "text-base-content/55": !issue(), truncate: r().kind !== "location" }}
                title={issue()}
              >
                {r().kind === "location" ? <DetailText text={node()!.contents} /> : r().kind === "group" && grp()!.components.length === 0 ? NO_COMPONENTS : (issue() ?? "")}
              </span>
              <span>
                {r().kind === "location"
                  ? <HealthSlots lights={node()!.lights} />
                  : r().kind === "component" ? <HealthSlots single={comp()!.health} /> : <HealthSlots single={grp()!.system!.health} />}
              </span>
              <span class="grid place-items-center" onClick={(e) => e.stopPropagation()}>
                <Show when={r().kind === "location" && addTargets(node()!)}>
                  {(targets) => (
                    <Show when={targets().length > 0}>
                      <AddMenu name={(r() as { node: LocationNode }).node.chain.map((c) => c.label).join(" / ")} targets={targets()} onAdd={props.onAdd} />
                    </Show>
                  )}
                </Show>
              </span>
            </div>
          );
        }}
      </For>
    </div>
  );
}

const KIND_LABEL: Record<AddKind, string> = { location: "Location", system: "System", component: "Component" };

type AddTarget = { id: string; label: string; kinds: AddKind[] };

function AddMenu(props: { name: string; targets: AddTarget[]; onAdd: (locationId: string, kind: AddKind) => void }) {
  const [open, setOpen] = createSignal(false);
  return (
    <Popover open={open()} onOpenChange={setOpen} placement="bottom-end" gutter={4}>
      <Popover.Trigger
        tabindex={-1}
        data-add
        class="grid h-6 w-6 cursor-pointer place-items-center rounded-md border border-base-content/15 text-base-content/70 opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100 data-[expanded]:opacity-100 hover:text-base-content"
        aria-label={`Add under ${props.name}`}
        title={`Add under ${props.name} (+)`}
      >
        <Plus size={13} />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content class="z-50 w-52 rounded-box border border-base-300 bg-base-100 p-1 shadow-2xl focus:outline-none">
          <For each={props.targets}>
            {(t, i) => (
              <div role="group" aria-label={`Under ${t.label}`} classList={{ "mt-1 border-t border-base-300 pt-1": i() > 0 }}>
                <p class="truncate px-2.5 pb-1 pt-1.5 text-xs text-base-content/50">Under {t.label}</p>
                <For each={t.kinds}>
                  {(k) => (
                    <button
                      type="button"
                      class="block w-full cursor-pointer rounded px-2.5 py-1.5 text-left text-sm hover:bg-base-content/[0.06]"
                      onClick={() => { setOpen(false); props.onAdd(t.id, k); }}
                    >
                      {KIND_LABEL[k]}
                    </button>
                  )}
                </For>
              </div>
            )}
          </For>
        </Popover.Content>
      </Popover.Portal>
    </Popover>
  );
}
