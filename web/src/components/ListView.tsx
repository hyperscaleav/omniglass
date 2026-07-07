import { type Accessor, type Component, type JSX, For, Show, createEffect, createMemo, createSignal, createUniqueId, onCleanup, untrack } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { facetActive as facetActiveFn, toggleFacet as toggleFacetFn, type Chip, type FilterKey } from "../lib/predicate";
import {
  buildIndex, pathOf as pathOfModel, flattenRows, treeRows, parsePref, toggleItem, moveItem, allExpanded as allExpandedModel,
  type Crumb, type Row, type SortState,
} from "../lib/listmodel";
import FilterBar from "./FilterBar";
import Drawer from "./Drawer";
import ColumnMenu from "./ColumnMenu";
import InfoTip from "./InfoTip";
import {
  ChevronDown, ChevronLeft, ChevronsDownUp, ChevronsUpDown, Columns, Check, ListTree, Rows, Maximize, Plus, Pencil, Trash, X,
} from "./icons";

// ListView: the one config-driven inventory shell. Every entity page (Components,
// Systems, Locations) is a config over this, never a fork. It owns the filter
// header (the faceted chip search), the action rail (view toggle, expand/collapse,
// column visibility, the primary create), the table in both tree and flattened
// modes, the stacked detail blades, the full-page detail shell, and the
// create/edit form Drawer. Filtering or list mode flattens the tree (with each
// row's ancestor path shown); sort is active only when flattened. Authorization is
// read off the caller's grants by the entity's resource name, a UI hint over the
// server's authority.
//
// The blade stack is the Azure model: a row opens an ephemeral right-hand blade,
// drilling into a child pushes another blade offset behind it, and Maximize
// promotes the blade to the addressable full-page URL. Blades carry no URL of
// their own (the full page is the shareable deep link); they stay live across a
// refetch by re-resolving their node id against the fresh index.
//
// Deferred to later phases on this branch: drag reorder of columns and sibling
// rows (Phase 6), and the summary widget board (Phase 7, where a health/alarm
// backend feeds it).

export interface ListNode {
  id: string;
  display: string;
  // polymorphic so a concrete node's children carry its own type (a CompNode's
  // children are CompNode[]), letting pages drill without casting.
  children: this[];
}

export type FormState<N> = { mode: "create"; parent: N | null } | { mode: "edit"; node: N };

export type Blade = { title: JSX.Element; headerExtra?: JSX.Element; body: JSX.Element };

// A summary widget: a compact badge (collapsed rail) and a full tile (expanded
// board). Both receive the context so a widget can drive the filter (facet
// toggles) or expand the rail.
export type Widget<N extends ListNode> = {
  title: string;
  badge: (ctx: ListCtx<N>) => JSX.Element;
  tile: (ctx: ListCtx<N>) => JSX.Element;
};

export type ListCtx<N extends ListNode> = {
  // true in the full-page detail, false in a blade: lets a shared detail body show
  // its breadcrumb only in a blade and drill via blade vs URL navigation.
  full: boolean;
  fact: (label: string, value: JSX.Element) => JSX.Element;
  field: (label: string, control: JSX.Element, hint?: string) => JSX.Element;
  facetActive: (key: string, val: string) => boolean;
  toggleFacet: (key: string, val: string) => void;
  openEdit: (n: N) => void;
  openCreate: (parent: N | null) => void;
  openNode: (n: N) => void;
  setSummaryOpen: (open: boolean) => void;
  // context-aware open: in a blade it pushes a child blade, on the full page it
  // navigates to that node's full-page URL.
  go: (n: N) => void;
  openFull: (n: N) => void;
  parentOf: (n: N) => N | undefined;
  byId: (id: string) => N | undefined;
  pushBlade: (n: N) => void;
  popBlade: () => void;
  closeBlades: () => void;
  setFullPage: (n: N | null) => void;
  pathOf: (n: N) => { id: string; display: string }[];
  nav?: (page: string, params?: unknown) => void;
};

export interface ListConfig<N extends ListNode> {
  // entity.name doubles as the authorization resource (component, system, location).
  entity: { name: string; plural: string };
  storageKey: string;
  flat?: boolean;
  nodes: Accessor<N[]>;
  focus?: Accessor<string | undefined>;
  loading?: Accessor<boolean>;
  error?: Accessor<unknown>;
  filterPlaceholder?: string;
  initialChips?: Chip[];
  columns: Record<string, { label: string; width: number }>;
  columnKeys: string[];
  defaultCols: string[];
  cellFor: (key: string, n: N, ctx: ListCtx<N>) => JSX.Element;
  filterKeys: FilterKey<N>[];
  sortVal: (n: N, key: string) => string | number;
  nameWeight?: (n: N) => number;
  canAddChild?: (n: N) => boolean;
  renderDetail: (n: N, ctx: ListCtx<N>) => JSX.Element;
  // when present, a row opens a blade instead of the full page; the body is the
  // same detail content, with a Maximize affordance in headerExtra.
  renderBlade?: (n: N, ctx: ListCtx<N>) => Blade;
  FormBody: Component<{ form: FormState<N>; close: () => void; ctx: ListCtx<N> }>;
  onOpenNode?: (n: N) => void;
  onBack?: () => void;
  onDelete?: (n: N, ctx: ListCtx<N>) => void;
  nav?: (page: string, params?: unknown) => void;
  // Optional summary board: a collapsible rail of widgets above the table, with a
  // personal show/hide preference. allWidgets is the catalog, defaultWidgets the
  // initial board.
  widgets?: Record<string, Widget<N>>;
  allWidgets?: string[];
  defaultWidgets?: string[];
}

// The static, page-agnostic part of an inventory page's config: the parts a matrix
// contract test can check without rendering. Each page exports one and spreads it
// into its ListConfig.
export type PageDescriptor = {
  entity: { name: string; plural: string };
  storageKey: string;
  columns: Record<string, { label: string; width: number }>;
  columnKeys: string[];
  defaultCols: string[];
};

export default function ListView<N extends ListNode>(props: { config: ListConfig<N> }) {
  const cfg = props.config;
  const me = useMe();
  const allow = (action: string) => can(me.data, cfg.entity.name, action);

  // The stored value is the visible columns IN ORDER (visibility + reorder in one
  // client preference; the eventual home is a per-principal user-preferences
  // endpoint, a straight read/write swap, not the cascade). parsePref keeps valid
  // keys in stored order, de-dupes, and honors an explicit empty array.
  const readCols = (): string[] => parsePref(localStorage.getItem(`${cfg.storageKey}-cols`), cfg.columnKeys) ?? cfg.defaultCols;
  const initialView = (): "tree" | "list" =>
    cfg.flat ? "list" : localStorage.getItem(`${cfg.storageKey}-view`) === "list" ? "list" : "tree";

  const [chips, setChips] = createSignal<Chip[]>(cfg.initialChips ?? []);
  const [expanded, setExpanded] = createSignal<Set<string>>(new Set());
  const [cols, setCols] = createSignal<string[]>(readCols());
  const [viewMode, setViewMode] = createSignal<"tree" | "list">(initialView());
  const [sort, setSort] = createSignal<SortState>(null);
  const [fullPage, setFullPage] = createSignal<N | null>(null);
  const [form, setForm] = createSignal<FormState<N> | null>(null);
  // The blade stack holds node ids, not node objects: ids are stable string values
  // so <For> keeps each blade's DOM across a refetch (which rebuilds node objects),
  // while the blade body re-resolves its node from the fresh index and updates in
  // place. Storing references here would tear down and remount every blade on any
  // mutation refetch.
  const [stack, setStack] = createSignal<string[]>([]);

  createEffect(() => localStorage.setItem(`${cfg.storageKey}-cols`, JSON.stringify(cols())));
  createEffect(() => localStorage.setItem(`${cfg.storageKey}-view`, viewMode()));

  // The signal setter treats a function arg as an updater; N is an object but TS
  // cannot prove it is not callable, so set the focused node through a thunk.
  const showFull = (n: N | null) => setFullPage(() => n);

  // The flattened index (id -> node, child -> parent, the in-order node list, the
  // container ids) and the ancestor path: both are pure, in lib/listmodel.
  const index = createMemo(() => buildIndex(cfg.nodes()));
  const pathOf = (n: N): Crumb[] => pathOfModel(index(), n);

  // After a refetch, drop any open blade whose node no longer exists (e.g. it was
  // deleted), and re-resolve the full-page node by id so it shows fresh data. The
  // blade bodies re-resolve themselves from index() by id, so surviving ids stay
  // put. fullPage is read untracked so this effect depends only on index().
  createEffect(() => {
    const idx = index();
    setStack((s) => {
      const next = s.filter((id) => idx.byId.has(id));
      return next.length === s.length ? s : next;
    });
    const fp = untrack(fullPage);
    if (fp) {
      const fresh = idx.byId.get(fp.id) ?? null;
      if (fresh !== fp) showFull(fresh);
    }
  });

  // Deep link: when the route carries a focus id, open that node full-page and
  // close any ephemeral blades (the full page is the addressable surface).
  createEffect(() => {
    const f = cfg.focus?.();
    if (!f) {
      showFull(null);
      return;
    }
    const n = index().byId.get(f) ?? null;
    showFull(n);
    if (n) setStack([]);
  });

  const filtering = createMemo(() => chips().length > 0);
  const flatten = createMemo(() => !!cfg.flat || filtering() || viewMode() === "list");
  // cols() IS the ordered visible list (so reorder persists); render in that order.
  const visible = createMemo(() => cols());

  // Flatten mode (filtering or list mode) compresses the tree to a flat list (tree
  // order by default, the chosen column sort otherwise); tree mode walks the
  // forest descending into expanded containers. Both derivations are pure.
  const rows = createMemo<Row<N>[]>(() =>
    flatten()
      ? flattenRows(index(), cfg.filterKeys, chips(), sort(), cfg.sortVal)
      : treeRows(cfg.nodes(), expanded()),
  );

  const toggleSort = (key: string) =>
    setSort((s) => (s?.key !== key ? { key, dir: 1 } : s.dir === 1 ? { key, dir: -1 } : null));
  const toggleNode = (id: string) =>
    setExpanded((s) => {
      const next = new Set(s);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  const allExpanded = createMemo(() => allExpandedModel(index().containerIds, expanded()));
  const toggleAll = () => setExpanded(allExpanded() ? new Set<string>() : new Set(index().containerIds));
  const toggleCol = (k: string) => setCols((c) => toggleItem(c, k));
  // Reorder a visible column from one position to another (drag in the menu).
  const moveCol = (from: number, to: number) => setCols((c) => moveItem(c, from, to));

  // Summary board: which widgets are on the personal board, and whether the rail is
  // expanded. Both persist as client preferences (same future home as columns).
  const readBoard = (): string[] => {
    if (!cfg.widgets) return [];
    const parsed = parsePref(localStorage.getItem(`${cfg.storageKey}-widgets`), Object.keys(cfg.widgets));
    return parsed ?? cfg.defaultWidgets ?? cfg.allWidgets ?? Object.keys(cfg.widgets);
  };
  const [board, setBoard] = createSignal<string[]>(readBoard());
  const [summaryOpen, setSummaryOpen] = createSignal(localStorage.getItem(`${cfg.storageKey}-sumopen`) === "1");
  const toggleWidget = (id: string) => setBoard((b) => toggleItem(b, id));
  if (cfg.widgets) {
    createEffect(() => localStorage.setItem(`${cfg.storageKey}-widgets`, JSON.stringify(board())));
    createEffect(() => localStorage.setItem(`${cfg.storageKey}-sumopen`, summaryOpen() ? "1" : "0"));
  }

  // Blade ops. pushBlade truncates to an existing entry (so a breadcrumb ancestor
  // collapses the stack back to it) or appends a new one.
  const pushBlade = (n: N) =>
    setStack((s) => {
      const i = s.indexOf(n.id);
      return i >= 0 ? s.slice(0, i + 1) : [...s, n.id];
    });
  const popBlade = () => setStack((s) => s.slice(0, -1));
  const closeBlades = () => setStack([]);

  const openFull = (n: N) => (cfg.onOpenNode ? cfg.onOpenNode(n) : showFull(n));
  // A row opens a blade when the config renders one, else the full page.
  const openNode = (n: N) => (cfg.renderBlade ? setStack([n.id]) : openFull(n));
  const back = () => (cfg.onBack ? cfg.onBack() : showFull(null));

  // Trap Tab within the top blade so focus cannot wander to the covered page.
  const trapTab = (e: KeyboardEvent, el: HTMLElement) => {
    if (e.key !== "Tab") return;
    const items = [...el.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),input,select,textarea,[tabindex]:not([tabindex="-1"])')].filter((x) => x.offsetParent !== null);
    if (!items.length) return;
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === el)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  };

  // Escape pops the top blade, unless a form Drawer is open over it (that dialog
  // owns Escape).
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && stack().length && !form()) {
      e.stopPropagation();
      popBlade();
    }
  };
  window.addEventListener("keydown", onKey);
  onCleanup(() => window.removeEventListener("keydown", onKey));

  // Focus management: when the stack opens, remember the element that had focus and
  // move focus into the top blade; when it closes, restore focus to that element.
  let priorFocus: HTMLElement | null = null;
  let wasOpen = false;
  createEffect(() => {
    const open = stack().length > 0;
    if (open && !wasOpen) priorFocus = document.activeElement as HTMLElement | null;
    else if (!open && wasOpen) {
      const el = priorFocus;
      priorFocus = null;
      queueMicrotask(() => el?.focus?.());
    }
    wasOpen = open;
    if (open) {
      queueMicrotask(() => {
        const asides = document.querySelectorAll<HTMLElement>("aside[data-blade]");
        asides[asides.length - 1]?.focus();
      });
    }
  });

  const baseCtx = {
    fact: (label: string, value: JSX.Element) => (
      <div>
        <div class="eyebrow mb-1.5">{label}</div>
        <div class="text-sm">{value}</div>
      </div>
    ),
    field: (label: string, control: JSX.Element, hint?: string) => {
      // Associate the visible label with the control by id, and keep the (i) help
      // affordance OUTSIDE the label: a labelable button inside the label would
      // steal the label's target and pollute the control's accessible name.
      const fieldId = createUniqueId();
      const target = control instanceof Element ? control
        : Array.isArray(control) ? control.find((c): c is Element => c instanceof Element)
        : undefined;
      if (target && !target.id) target.id = fieldId;
      return (
        <div class="flex flex-col gap-1.5">
          <span class="flex items-center gap-1.5">
            <label class="eyebrow" for={target?.id ?? fieldId}>{label}</label>
            <Show when={hint}><InfoTip text={hint!} label={label} /></Show>
          </span>
          {control}
        </div>
      );
    },
    facetActive: (key: string, val: string) => facetActiveFn(chips(), key, val),
    toggleFacet: (key: string, val: string) => setChips(toggleFacetFn(chips(), key, val)),
    // Close any open blade before the form Drawer opens (the Drawer would
    // otherwise render behind the higher-z blade and be unreachable).
    openEdit: (n: N) => { closeBlades(); setForm({ mode: "edit", node: n }); },
    openCreate: (parent: N | null) => { closeBlades(); setForm({ mode: "create", parent }); },
    openNode,
    setSummaryOpen,
    openFull,
    parentOf: (n: N) => index().parentOf.get(n.id),
    byId: (id: string) => index().byId.get(id),
    pushBlade,
    popBlade,
    closeBlades,
    setFullPage: (n: N | null) => showFull(n),
    pathOf,
    nav: cfg.nav,
  };
  // Two views over the shared context: the full page navigates by URL, a blade
  // drills by pushing another blade.
  const ctxFull: ListCtx<N> = { ...baseCtx, full: true, go: openFull };
  const ctxBlade: ListCtx<N> = { ...baseCtx, full: false, go: pushBlade };

  const colBox = (on: boolean) =>
    "flex h-4 w-4 flex-none items-center justify-center rounded border " +
    (on ? "border-primary bg-primary text-primary-content" : "border-base-300");

  const actions = (
    <>
      <Show when={!cfg.flat}>
        <button
          class="btn btn-quiet btn-sm btn-square"
          title={viewMode() === "list" ? "Switch to tree view" : "Switch to list view"}
          onClick={() => setViewMode(viewMode() === "list" ? "tree" : "list")}
        >
          {viewMode() === "list" ? <ListTree size={15} /> : <Rows size={15} />}
        </button>
      </Show>
      <Show when={!flatten()}>
        <button class="btn btn-quiet btn-sm btn-square" title={allExpanded() ? "Collapse all" : "Expand all"} onClick={toggleAll}>
          {allExpanded() ? <ChevronsDownUp size={15} /> : <ChevronsUpDown size={15} />}
        </button>
      </Show>
      <ColumnMenu columns={cfg.columns} columnKeys={cfg.columnKeys} cols={cols} onToggle={toggleCol} onMove={moveCol} />
      <span class="mx-1 h-5 w-px flex-none bg-base-300" />
      <Show when={allow("create")}>
        <button class="btn btn-action btn-sm" onClick={() => ctxFull.openCreate(null)}>
          <Plus size={15} /> New {cfg.entity.name}
        </button>
      </Show>
    </>
  );

  const Th = (p: { col: string; label: string }) => {
    const sortable = () => flatten();
    const active = () => sort()?.key === p.col;
    return (
      <th
        class="sticky top-0 z-5 select-none bg-base-200 text-left"
        classList={{ "cursor-pointer": sortable() }}
        onClick={() => sortable() && toggleSort(p.col)}
      >
        <span class="inline-flex items-center gap-1">
          {p.label}
          <Show when={sortable() && active()}>
            <span class="text-[10px] text-primary">{sort()!.dir > 0 ? "▲" : "▼"}</span>
          </Show>
        </span>
      </th>
    );
  };

  const RowEl = (p: { row: Row<N> }) => {
    const n = p.row.n;
    const isTree = () => !flatten();
    const kids = () => (n.children as N[]) ?? [];
    const canExpand = () => isTree() && kids().length > 0;
    const open = () => expanded().has(n.id);
    return (
      <tr
        class="group cursor-pointer hover:bg-base-content/5 focus-visible:bg-base-content/5 focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary"
        role="button"
        tabindex={0}
        onClick={() => openNode(n)}
        onKeyDown={(e) => {
          if ((e.key === "Enter" || e.key === " ") && e.currentTarget === e.target) {
            e.preventDefault();
            openNode(n);
          }
        }}
      >
        <td>
          <span class="inline-flex w-full items-center gap-1.5" style={{ "padding-left": isTree() ? `${p.row.depth * 20}px` : "0" }}>
            <Show when={isTree()}>
              <span class="inline-flex w-4 flex-none justify-center text-base-content/40">
                <Show when={canExpand()} fallback={<span class="font-data text-[11px]">{"·"}</span>}>
                  <button
                    class="inline-flex"
                    title={open() ? "Collapse" : "Expand"}
                    style={{ transform: open() ? "none" : "rotate(-90deg)", transition: "transform .15s" }}
                    onClick={(e) => {
                      e.stopPropagation();
                      toggleNode(n.id);
                    }}
                  >
                    <ChevronDown size={14} />
                  </button>
                </Show>
              </span>
            </Show>
            <span class="flex min-w-0 flex-col gap-0.5 py-0.5">
              <Show when={p.row.path && p.row.path.length}>
                <span class="truncate text-[11px] text-base-content/40">{p.row.path!.map((x) => x.display).join(" › ")}</span>
              </Show>
              <span class="truncate" style={{ "font-weight": cfg.nameWeight ? cfg.nameWeight(n) : 500 }}>
                {n.display}
              </span>
            </span>
          </span>
        </td>
        <For each={visible()}>
          {(k) => <td class="overflow-hidden text-ellipsis whitespace-nowrap text-sm">{cfg.cellFor(k, n, ctxFull)}</td>}
        </For>
        <td>
          <div class="flex justify-end gap-0.5 opacity-0 transition group-hover:opacity-100 group-focus-within:opacity-100">
            <button class="btn btn-quiet btn-xs btn-square" title="Open full page" onClick={(e) => { e.stopPropagation(); openFull(n); }}>
              <Maximize size={15} />
            </button>
            <Show when={cfg.canAddChild?.(n) && allow("create")}>
              <button class="btn btn-quiet btn-xs btn-square" title="Add child" onClick={(e) => { e.stopPropagation(); ctxFull.openCreate(n); }}>
                <Plus size={15} />
              </button>
            </Show>
            <Show when={allow("update")}>
              <button class="btn btn-quiet btn-xs btn-square" title="Edit" onClick={(e) => { e.stopPropagation(); ctxFull.openEdit(n); }}>
                <Pencil size={15} />
              </button>
            </Show>
            <Show when={allow("delete") && cfg.onDelete}>
              <button class="btn btn-danger btn-xs btn-square" title="Delete" onClick={(e) => { e.stopPropagation(); cfg.onDelete!(n, ctxFull); }}>
                <Trash size={15} />
              </button>
            </Show>
          </div>
        </td>
      </tr>
    );
  };

  // Summary board: a collapsed badge rail or an expanded tile grid, with a personal
  // show/hide preference. Rendered above the table only when the config supplies
  // widgets.
  const SummaryRail = () => {
    const W = cfg.widgets!;
    const catalog = cfg.allWidgets ?? Object.keys(W);
    return (
      <div class="flex flex-col gap-3">
        <Show
          when={summaryOpen()}
          fallback={
            <div class="flex flex-wrap items-center gap-2">
              <div class="flex min-w-0 flex-1 flex-wrap gap-2">
                <For each={board().filter((id) => W[id])}>{(id) => W[id].badge(ctxFull)}</For>
              </div>
              <button class="btn btn-quiet btn-sm btn-square flex-none" title="Expand summary" onClick={() => setSummaryOpen(true)}>
                <span class="inline-flex" style={{ transform: "rotate(-90deg)" }}><ChevronDown size={16} /></span>
              </button>
            </div>
          }
        >
          <div class="flex items-center gap-2">
            <span class="eyebrow">Summary</span>
            <span class="flex-1" />
            <details class="dropdown dropdown-end">
              <summary class="btn btn-quiet btn-sm gap-1.5"><Columns size={14} /> Customize</summary>
              <ul class="dropdown-content menu z-40 mt-1.5 w-56 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl">
                <li class="menu-title px-2 pb-1.5 text-[10.5px]">Show widgets · personal</li>
                <For each={catalog}>
                  {(id) => (
                    <li>
                      <button class="flex items-center gap-2.5 px-2 py-1.5" onClick={() => toggleWidget(id)}>
                        <span class={colBox(board().includes(id))}><Show when={board().includes(id)}><Check size={11} /></Show></span>
                        {W[id].title}
                      </button>
                    </li>
                  )}
                </For>
              </ul>
            </details>
            <button class="btn btn-quiet btn-sm gap-1.5" onClick={() => setSummaryOpen(false)}>Collapse <ChevronDown size={14} /></button>
          </div>
          <div class="flex flex-wrap items-stretch gap-3">
            <For each={board().filter((id) => W[id])}>
              {(id) => <div class="min-w-50 max-w-sm flex-[1_1_220px]">{W[id].tile(ctxFull)}</div>}
            </For>
          </div>
        </Show>
      </div>
    );
  };

  const ListBody = () => (
    <section class="fade-in flex flex-col gap-3.5">
      <Show when={cfg.widgets}>
        <SummaryRail />
      </Show>
      <div class="card overflow-hidden border border-base-300 bg-base-200 p-0">
        <div class="border-b border-base-300 px-3 py-2.5">
          <FilterBar
            keys={cfg.filterKeys}
            rows={index().all}
            chips={chips()}
            onChips={setChips}
            bare
            clearable
            trailing={actions}
            placeholder={cfg.filterPlaceholder}
          />
        </div>
        <div class="overflow-x-auto">
          <table class="og-rows table table-fixed table-sm">
            <colgroup>
              <col />
              <For each={visible()}>{(k) => <col style={{ width: `${cfg.columns[k].width}px` }} />}</For>
              <col style={{ width: "104px" }} />
            </colgroup>
            <thead>
              <tr>
                <Th col="name" label="Name" />
                <For each={visible()}>{(k) => <Th col={k} label={cfg.columns[k].label} />}</For>
                <th class="sticky top-0 z-5 bg-base-200" />
              </tr>
            </thead>
            <tbody>
              <Show
                when={rows().length}
                fallback={
                  <tr>
                    <td colspan={visible().length + 2} class="py-8 text-center text-base-content/50">
                      No {cfg.entity.plural.toLowerCase()} match the filter.
                    </td>
                  </tr>
                }
              >
                <For each={rows()}>{(r) => <RowEl row={r} />}</For>
              </Show>
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );

  const FullPage = (props2: { node: N }) => (
    <section class="fade-in flex max-w-3xl flex-col gap-4">
      <button class="btn btn-quiet btn-sm flex-none gap-1.5 self-start" onClick={back}>
        {"←"} {cfg.entity.plural}
      </button>
      <div class="flex flex-col gap-2">
        <Show when={pathOf(props2.node).length}>
          <div class="flex flex-wrap items-center gap-1 text-[11.5px]">
            <For each={pathOf(props2.node)}>
              {(c, i) => (
                <>
                  <Show when={i()}>
                    <span class="text-base-content/30">{"›"}</span>
                  </Show>
                  <button
                    class="text-base-content/60 hover:text-base-content"
                    onClick={() => {
                      const anc = index().byId.get(c.id);
                      if (anc) openFull(anc);
                    }}
                  >
                    {c.display}
                  </button>
                </>
              )}
            </For>
          </div>
        </Show>
        <h1 class="text-2xl font-semibold tracking-tight">{props2.node.display}</h1>
      </div>
      <div class="card border border-base-300 bg-base-200 og-pad">{cfg.renderDetail(props2.node, ctxFull)}</div>
    </section>
  );

  const BladeStack = () => {
    const top = () => stack().length - 1;
    return (
      <Show when={stack().length}>
        <div class="fixed inset-0 z-60 bg-black/45" onClick={closeBlades} />
        <For each={stack()}>
          {(id, i) => {
            const node = () => index().byId.get(id);
            const isTop = () => i() === top();
            const titleId = `blade-title-${id}`;
            return (
              <Show when={node()}>
                {(n) => {
                  // Recomputes only when this node's data changes (its id is stable,
                  // so the aside DOM persists across a refetch).
                  const blade = createMemo(() => cfg.renderBlade!(n(), ctxBlade));
                  return (
                    <aside
                      data-blade
                      tabindex={-1}
                      role="dialog"
                      aria-modal={isTop() ? "true" : undefined}
                      aria-labelledby={titleId}
                      class="fixed inset-y-0 flex w-full max-w-md flex-col border-l border-base-300 bg-base-100 shadow-2xl outline-none"
                      style={{ right: `${(top() - i()) * 40}px`, "z-index": 61 + i() }}
                      onKeyDown={(e) => isTop() && trapTab(e, e.currentTarget)}
                    >
                      <header class="flex items-center justify-between gap-3 border-b border-base-300 px-4 py-3">
                        <div class="flex min-w-0 items-center gap-2">
                          <Show when={i()}>
                            <button class="btn btn-quiet btn-sm btn-square" title="Back" onClick={popBlade}>
                              <ChevronLeft size={16} />
                            </button>
                          </Show>
                          <div id={titleId} class="min-w-0 truncate text-sm font-semibold">{blade().title}</div>
                        </div>
                        <div class="flex flex-none items-center gap-1">
                          {blade().headerExtra}
                          <button class="btn btn-quiet btn-sm btn-square" title="Close" aria-label="Close" onClick={closeBlades}>
                            <X size={16} />
                          </button>
                        </div>
                      </header>
                      <div class="flex-1 overflow-auto p-5" classList={{ "pointer-events-none opacity-55": !isTop() }}>
                        {blade().body}
                      </div>
                      <Show when={!isTop()}>
                        <div class="absolute inset-0 cursor-pointer" onClick={() => setStack((s) => s.slice(0, i() + 1))} />
                      </Show>
                    </aside>
                  );
                }}
              </Show>
            );
          }}
        </For>
      </Show>
    );
  };

  return (
    <>
      <Show when={cfg.error?.()}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm">
          <span>Could not load {cfg.entity.plural.toLowerCase()}: {describeError(cfg.error?.())}</span>
        </div>
      </Show>
      <Show when={fullPage()} fallback={<ListBody />}>
        {(n) => <FullPage node={n()} />}
      </Show>
      <BladeStack />
      <Show when={form()}>
        {(f) => (
          <Drawer open={true} onClose={() => setForm(null)} title={f().mode === "edit" ? `Edit ${cfg.entity.name}` : `New ${cfg.entity.name}`}>
            <Dynamic component={cfg.FormBody} form={f()} close={() => setForm(null)} ctx={ctxFull} />
          </Drawer>
        )}
      </Show>
    </>
  );
}
