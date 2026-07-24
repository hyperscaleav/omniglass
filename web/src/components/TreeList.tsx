import { type Accessor, type Component, type JSX, For, Show, createEffect, createMemo, createSignal, untrack } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { facetActive as facetActiveFn, toggleFacet as toggleFacetFn, resolveFilterKeys, type Chip, type FilterKeys } from "../lib/predicate";
import {
  buildIndex, pathOf as pathOfModel, flattenRows, treeRows, parsePref, toggleItem, moveItem, allExpanded as allExpandedModel,
  type Crumb, type Row, type SortState,
} from "../lib/listmodel";
import ListShell from "./ListShell";
import Drawer from "./Drawer";
import ColumnMenu from "./ColumnMenu";
import FieldRow from "./FieldRow";
import {
  ChevronDown, ChevronLeft, ChevronsDownUp, ChevronsUpDown, Columns, Check, ListTree, Rows, Maximize, Plus, Pencil, Trash,
} from "./icons";
import BladeStack from "./BladeStack";
import Button from "./Button";
import KVStacked from "./KVStacked";
import { type BladeDef, type BladeEdit, type BladeRef, BladesContext, createBladeController, createEditSlot, useBladeEdit } from "../lib/blades";

// TreeList: the one config-driven tree-list body (composing ListShell), the inventory shell. Every entity page (Components,
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
  // The scope-aware actions the server says the caller may perform on THIS row
  // (create a child, update, delete), from the read-side `actions` field. When
  // present, the row gates its affordances on this rather than the coarse
  // capability, so a scoped operator sees only the buttons the server would allow.
  actions?: string[];
  // polymorphic so a concrete node's children carry its own type (a CompNode's
  // children are CompNode[]), letting pages drill without casting.
  children: this[];
}

export type FormState<N> = { mode: "create"; parent: N | null } | { mode: "edit"; node: N };

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
  // The read -> Edit -> Save slot for THIS detail surface, present only when the ctx
  // renders a detail body (the blade slot inside EntityBladeBody, or the full page's
  // own slot); absent in list-cell / create contexts. A detail body reads
  // `edit.editing()` to switch view (read-only) vs edit (inputs + binding editors),
  // `edit.bind()` to register its saver, and `edit.begin()` for its pencil. Threaded
  // through ctx, never via useBladeEdit, since renderDetail is shared by the blade
  // (inside a provider) and the full page (outside one).
  edit?: BladeEdit;
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
  // Push an arbitrary blade (a kind registered via ListConfig.extraBlades) onto
  // the shared stack, so a detail body can open a non-node blade that nests in
  // the same stack rather than a separate overlay (which a higher-z blade hides).
  openBlade: (ref: BladeRef) => void;
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
  filterKeys: FilterKeys<N>;
  sortVal: (n: N, key: string) => string | number;
  nameWeight?: (n: N) => number;
  // Optional leading glyph rendered immediately before a node's name in the tree,
  // always visible (not a toggleable column). Used by Locations to wear each
  // location's type icon so a campus reads differently from a building at a glance.
  leadIcon?: (n: N) => JSX.Element;
  canAddChild?: (n: N) => boolean;
  renderDetail: (n: N, ctx: ListCtx<N>) => JSX.Element;
  // Extra blade kinds this page's detail body can open on the shared stack (via
  // ctx.openBlade), keyed by kind, alongside the page's own entity blade. Used by
  // Components to open a secret's cascade as a nested blade.
  extraBlades?: Record<string, BladeDef>;
  // The create/edit Drawer body. Optional: a page on the create-as-route model omits
  // it (create is renderCreate at /<entity>/create, edit is inline on the detail
  // accordion), so the Drawer never opens. Pages still on the drawer model provide it.
  FormBody?: Component<{ form: FormState<N>; close: () => void; ctx: ListCtx<N> }>;
  // The draft-create surface, rendered full-page when the focus id is the reserved
  // "create" (from /<entity>/create). It owns the draft fields and, on Save, navigates
  // to /<entity>/<newId>. When set, `New` should route here via onNew.
  renderCreate?: (ctx: ListCtx<N>) => JSX.Element;
  // What the `New <entity>` button (and a row's Add-child) does. Defaults to opening
  // the create Drawer; a create-as-route page overrides it to navigate to
  // /<entity>/create.
  onNew?: () => void;
  // What a row's Edit pencil does. Defaults to opening the edit Drawer; a
  // create-as-route page overrides it to open the node's detail in edit (navigate +
  // a pending-edit handoff). The blade / full-page pencils drive the edit slot
  // directly, so this is only the list-row affordance.
  onEdit?: (n: N) => void;
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

export default function TreeList<N extends ListNode>(props: { config: ListConfig<N> }) {
  const cfg = props.config;
  const me = useMe();
  const allow = (action: string) => can(me.data, cfg.entity.name, action);
  // Per-row gating: when the server annotated the row with scope-aware `actions`,
  // honor it (a scoped operator sees only what it may do to THIS row); otherwise
  // fall back to the coarse capability. The server is always the authority.
  const rowAllow = (n: N, action: string) => (n.actions ? n.actions.includes(action) : allow(action));

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
  // The cross-entity blade stack (shared primitive). Holds { kind, id } refs; this
  // page only ever pushes its own entity kind, and the blade body re-resolves the
  // node from the fresh index by id so a surviving blade stays live across a
  // refetch. See lib/blades and components/BladeStack.
  const blades = createBladeController();

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
    blades.filter((r) => idx.byId.has(r.id));
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
    if (n) blades.close();
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
      ? flattenRows(index(), resolveFilterKeys(cfg.filterKeys), chips(), sort(), cfg.sortVal)
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

  // Blade ops delegate to the shared controller; a node maps to a { kind, id } ref
  // under this page's entity kind. push truncates-to-existing (breadcrumb collapse).
  const pushBlade = (n: N) => blades.push({ kind: cfg.entity.name, id: n.id });
  const popBlade = () => blades.pop();
  const closeBlades = () => blades.close();

  const openFull = (n: N) => (cfg.onOpenNode ? cfg.onOpenNode(n) : showFull(n));
  // A row opens a blade; Maximize promotes it to the addressable full page.
  const openNode = (n: N) => pushBlade(n);
  const back = () => (cfg.onBack ? cfg.onBack() : showFull(null));

  const baseCtx = {
    fact: (label: string, value: JSX.Element) => <KVStacked label={label} value={value} />,
    // The blade forms render their fields through the shared FieldRow wrapper.
    // TreeList's `hint` is the (i) tooltip text (not a below-field hint), so it
    // maps to FieldRow's `info`.
    field: (label: string, control: JSX.Element, hint?: string) =>
      <FieldRow label={label} info={hint}>{control}</FieldRow>,
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
    openBlade: (ref: BladeRef) => blades.push(ref),
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

  // The entity blade body: renderDetail in blade context, plus the footer action
  // rail. Delete and Edit register on the shared BladeStack footer (pinned to the
  // blade bottom, like the identity blades) rather than an inline bar that scrolls
  // with the body; the page's detail() renders its inline bar only on the full
  // page (ctx.full). Gated per row by the server's scope-aware actions, and Edit
  // opens the form Drawer (the inventory edit flow, not the inline-pencil model).
  const EntityBladeBody = (p: { id: string }) => {
    const edit = useBladeEdit();
    const node = () => index().byId.get(p.id);
    edit.bind({
      destructive: () => {
        const n = node();
        return n && cfg.onDelete && rowAllow(n, "delete")
          ? { label: "Delete", tone: "danger" as const, onClick: () => cfg.onDelete!(n, ctxBlade) }
          : undefined;
      },
      primary: () => {
        const n = node();
        return n && rowAllow(n, "update")
          ? { label: "Edit", icon: <Pencil size={15} />, onClick: () => ctxBlade.openEdit(n) }
          : undefined;
      },
    });
    return <Show when={node()}>{(n) => cfg.renderDetail(n(), { ...ctxBlade, edit })}</Show>;
  };

  // Single-kind registry for the shared BladeStack: this page's own entity. The
  // title is the node display; the body is renderDetail in blade context (drills by
  // pushing a child blade); Maximize promotes the blade to the addressable full page.
  const bladeRegistry: Record<string, BladeDef> = {
    ...(cfg.extraBlades ?? {}),
    [cfg.entity.name]: {
      Title: (p) => <>{index().byId.get(p.id)?.display}</>,
      Body: (p) => <EntityBladeBody id={p.id} />,
      headerExtra: (p) => (
        <Show when={index().byId.get(p.id)}>
          {(n) => (
            <Button square icon={Maximize} title="Open full page" label="Open full page" onClick={() => { blades.close(); openFull(n()); }} />
          )}
        </Show>
      ),
    },
  };

  const colBox = (on: boolean) =>
    "flex h-4 w-4 flex-none items-center justify-center rounded border " +
    (on ? "border-primary bg-primary text-primary-content" : "border-base-300");

  const actions = (
    <>
      <Show when={!cfg.flat}>
        <Button
          square
          icon={viewMode() === "list" ? ListTree : Rows}
          title={viewMode() === "list" ? "Switch to tree view" : "Switch to list view"}
          onClick={() => setViewMode(viewMode() === "list" ? "tree" : "list")}
        />
      </Show>
      <Show when={!flatten()}>
        <Button square icon={allExpanded() ? ChevronsDownUp : ChevronsUpDown} title={allExpanded() ? "Collapse all" : "Expand all"} onClick={toggleAll} />
      </Show>
      <ColumnMenu columns={cfg.columns} columnKeys={cfg.columnKeys} cols={cols} onToggle={toggleCol} onMove={moveCol} />
      <span class="mx-1 h-5 w-px flex-none bg-base-300" />
      <Show when={allow("create")}>
        <Button intent="action" icon={Plus} onClick={() => (cfg.onNew ? cfg.onNew() : ctxFull.openCreate(null))}>New {cfg.entity.name}</Button>
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
        <td class="min-w-52">
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
            <Show when={cfg.leadIcon}>
              <span class="inline-flex flex-none items-center">{cfg.leadIcon!(n)}</span>
            </Show>
            <span class="flex min-w-0 flex-col gap-0.5 py-0.5">
              <Show when={p.row.path && p.row.path.length}>
                <span class="truncate text-[11px] text-base-content/40">{p.row.path!.map((x) => x.display).join(" › ")}</span>
              </Show>
              {/* The label, then the key beneath it. A row's id IS its key (the
                  kebab name the API and CLI address it by), so an operator can
                  read what to type without opening the row. It is always shown
                  rather than revealed on hover: hover does not exist on touch,
                  is not discoverable, and cannot be selected to copy, and
                  copying it into a CLI invocation is the point.

                  When the entity has no display name the label IS the key, so
                  it is rendered once, in the data face, which marks it as an
                  identifier rather than a name somebody chose. */}
              <span
                class="truncate"
                classList={{ "font-data text-[13px]": n.display === n.id }}
                style={{ "font-weight": cfg.nameWeight ? cfg.nameWeight(n) : 500 }}
              >
                {n.display}
              </span>
              <Show when={n.display !== n.id}>
                <span class="truncate font-data text-[11px] text-base-content/40">{n.id}</span>
              </Show>
            </span>
          </span>
        </td>
        <For each={visible()}>
          {(k) => <td class="overflow-hidden text-ellipsis whitespace-nowrap text-sm">{cfg.cellFor(k, n, ctxFull)}</td>}
        </For>
        <td>
          <div class="flex justify-end gap-0.5 opacity-0 transition group-hover:opacity-100 group-focus-within:opacity-100">
            <Button square size="xs" icon={Maximize} title="Open full page" onClick={(e) => { e.stopPropagation(); openFull(n); }} />
            <Show when={cfg.canAddChild?.(n) && rowAllow(n, "create")}>
              <Button square size="xs" icon={Plus} title="Add child" onClick={(e) => { e.stopPropagation(); if (cfg.onNew) cfg.onNew(); else ctxFull.openCreate(n); }} />
            </Show>
            <Show when={rowAllow(n, "update")}>
              <Button square size="xs" icon={Pencil} title="Edit" onClick={(e) => { e.stopPropagation(); if (cfg.onEdit) cfg.onEdit(n); else ctxFull.openEdit(n); }} />
            </Show>
            <Show when={rowAllow(n, "delete") && cfg.onDelete}>
              <Button square size="xs" intent="danger" icon={Trash} title="Delete" onClick={(e) => { e.stopPropagation(); cfg.onDelete!(n, ctxFull); }} />
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
              <Button square icon={ChevronLeft} title="Expand summary" label="Expand summary" class="flex-none" onClick={() => setSummaryOpen(true)} />
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
            <Button icon={ChevronDown} iconTrailing onClick={() => setSummaryOpen(false)}>Collapse</Button>
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
      {/* The chrome (FilterBar, card, action rail) is the shared ListShell; the
          tree body owns its own chips (controlled), since it filters tree-aware
          via flattenRows rather than the shell's flat predicate. */}
      <ListShell
        filterKeys={resolveFilterKeys(cfg.filterKeys)}
        rows={index().all}
        chips={chips}
        onChips={setChips}
        trailing={actions}
        placeholder={cfg.filterPlaceholder}
      >
        {() => (
          <div class="overflow-x-auto">
            <table class="og-rows table table-fixed table-sm">
            <colgroup>
              <col />
              <For each={visible()}>{(k) => <col style={{ width: `${cfg.columns[k].width}px` }} />}</For>
              <col style={{ width: "150px" }} />
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
        )}
      </ListShell>
    </section>
  );

  const FullPage = (props2: { node: N }) => {
    // The full page hosts its own read -> Edit -> Save slot (the blade gets one from
    // BladeStack; the full page renders outside BladeStack, so it makes its own). The
    // page's detail() renders the Save / Cancel / pencil footer from ctx.edit.
    const edit = createEditSlot();
    return (
    <section class="fade-in flex max-w-3xl flex-col gap-4">
      <Button class="flex-none self-start" onClick={back}>{"←"} {cfg.entity.plural}</Button>
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
      <div class="card border border-base-300 bg-base-200 og-pad">
        {/* Wrap in a Show so the detail body mounts once per full-page visit and is not
            re-executed (which would discard unsaved edit-mode input state) when a
            background list refetch swaps props2.node for a fresh object; Show dedupes on
            truthiness, so the callback runs once while the node stays present. The detail
            re-reads live data through ctx.byId. Mirrors the blade path (EntityBladeBody). */}
        <Show when={props2.node}>{(node) => cfg.renderDetail(node(), { ...ctxFull, edit })}</Show>
      </div>
    </section>
    );
  };

  // The reserved focus id "create" (from /<entity>/create) shows the draft-create
  // surface full-page instead of the list. renderDetail resolves a real id; renderCreate
  // owns a not-yet-saved draft and, on Save, navigates to /<entity>/<newId>.
  const isCreate = () => cfg.focus?.() === "create" && !!cfg.renderCreate;

  return (
    // The blade controller is shared through context, not just handed to BladeStack:
    // a blade BODY (an interface blade drilling to its component) calls useBlades to
    // push or pop, and without the provider it throws before it renders. FlatList has
    // always done this; TreeList did not, which left both interface blades dead on
    // every TreeList page (#336).
    <BladesContext.Provider value={blades}>
      <Show when={cfg.error?.()}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm">
          <span>Could not load {cfg.entity.plural.toLowerCase()}: {describeError(cfg.error?.())}</span>
        </div>
      </Show>
      <Show
        when={isCreate()}
        fallback={
          <Show when={fullPage()} fallback={<ListBody />}>
            {(n) => <FullPage node={n()} />}
          </Show>
        }
      >
        <section class="fade-in flex max-w-3xl flex-col gap-4">
          <Button class="flex-none self-start" onClick={back}>{"←"} {cfg.entity.plural}</Button>
          <div class="card border border-base-300 bg-base-200 og-pad">{cfg.renderCreate!(ctxFull)}</div>
        </section>
      </Show>
      <BladeStack controller={blades} registry={bladeRegistry} />
      <Show when={form() && cfg.FormBody}>
        {(FB) => (
          <Drawer open={true} onClose={() => setForm(null)} title={form()!.mode === "edit" ? `Edit ${cfg.entity.name}` : `New ${cfg.entity.name}`}>
            <Dynamic component={FB()} form={form()!} close={() => setForm(null)} ctx={ctxFull} />
          </Drawer>
        )}
      </Show>
    </BladesContext.Provider>
  );
}
