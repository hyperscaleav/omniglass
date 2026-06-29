import { type Accessor, type Component, type JSX, For, Show, createEffect, createMemo, createSignal } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useMe, can } from "../lib/auth";
import { buildPredicate, facetActive as facetActiveFn, toggleFacet as toggleFacetFn, type Chip, type FilterKey } from "../lib/predicate";
import FilterBar from "./FilterBar";
import Drawer from "./Drawer";
import {
  ChevronDown, ChevronsDownUp, ChevronsUpDown, Columns, Check, ListTree, Rows, Maximize, Plus, Pencil, Trash,
} from "./icons";

// ListView: the one config-driven inventory shell. Every entity page (Components,
// Systems, Locations) is a config over this, never a fork. It owns the filter
// header (the faceted chip search), the action rail (view toggle, expand/collapse,
// column visibility, the primary create), the table in both tree and flattened
// modes, the full-page detail shell, and the create/edit form Drawer. Filtering or
// list mode flattens the tree (with each row's ancestor path shown); sort is
// active only when flattened. Authorization is read off the caller's grants by
// the entity's resource name, a UI hint over the server's authority.
//
// Deferred to later phases on this branch: the stacked detail blades (Phase 5),
// drag reorder of columns and sibling rows (Phase 6), and the summary widget
// board (Phase 7, where a health/alarm backend feeds it).

export interface ListNode {
  id: string;
  display: string;
  children: ListNode[];
}

export type FormState<N> = { mode: "create"; parent: N | null } | { mode: "edit"; node: N };

export type ListCtx<N extends ListNode> = {
  fact: (label: string, value: JSX.Element) => JSX.Element;
  field: (label: string, control: JSX.Element, hint?: string) => JSX.Element;
  facetActive: (key: string, val: string) => boolean;
  toggleFacet: (key: string, val: string) => void;
  openEdit: (n: N) => void;
  openCreate: (parent: N | null) => void;
  openNode: (n: N) => void;
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
  FormBody: Component<{ form: FormState<N>; close: () => void; ctx: ListCtx<N> }>;
  onOpenNode?: (n: N) => void;
  onBack?: () => void;
  onDelete?: (n: N, ctx: ListCtx<N>) => void;
  nav?: (page: string, params?: unknown) => void;
}

type Crumb = { id: string; display: string };
type Row<N> = { n: N; depth: number; path: Crumb[] | null };

export default function ListView<N extends ListNode>(props: { config: ListConfig<N> }) {
  const cfg = props.config;
  const me = useMe();
  const allow = (action: string) => can(me.data, cfg.entity.name, action);

  const readCols = (): string[] => {
    try {
      const raw = localStorage.getItem(`${cfg.storageKey}-cols`);
      if (raw) {
        const arr = JSON.parse(raw);
        if (Array.isArray(arr)) return cfg.columnKeys.filter((k) => arr.includes(k));
      }
    } catch {
      /* ignore corrupt prefs */
    }
    return cfg.defaultCols;
  };
  const initialView = (): "tree" | "list" =>
    cfg.flat ? "list" : localStorage.getItem(`${cfg.storageKey}-view`) === "list" ? "list" : "tree";

  const [chips, setChips] = createSignal<Chip[]>(cfg.initialChips ?? []);
  const [expanded, setExpanded] = createSignal<Set<string>>(new Set());
  const [cols, setCols] = createSignal<string[]>(readCols());
  const [viewMode, setViewMode] = createSignal<"tree" | "list">(initialView());
  const [sort, setSort] = createSignal<{ key: string; dir: 1 | -1 } | null>(null);
  const [fullPage, setFullPage] = createSignal<N | null>(null);
  const [form, setForm] = createSignal<FormState<N> | null>(null);

  createEffect(() => localStorage.setItem(`${cfg.storageKey}-cols`, JSON.stringify(cols())));
  createEffect(() => localStorage.setItem(`${cfg.storageKey}-view`, viewMode()));

  // The signal setter treats a function arg as an updater; N is an object but TS
  // cannot prove it is not callable, so set the focused node through a thunk.
  const showFull = (n: N | null) => setFullPage(() => n);

  // The flattened index: id -> node, child -> parent, the in-order node list, and
  // the set of nodes that have children (containers, for expand/collapse-all).
  type Idx = { byId: Map<string, N>; parentOf: Map<string, N>; all: N[]; containerIds: Set<string> };
  const index = createMemo<Idx>(() => {
    const byId = new Map<string, N>();
    const parentOf = new Map<string, N>();
    const all: N[] = [];
    const containerIds = new Set<string>();
    const walk = (list: N[], parent: N | null) => {
      for (const n of list) {
        byId.set(n.id, n);
        all.push(n);
        if (parent) parentOf.set(n.id, parent);
        const kids = (n.children as N[]) ?? [];
        if (kids.length) {
          containerIds.add(n.id);
          walk(kids, n);
        }
      }
    };
    walk(cfg.nodes(), null);
    return { byId, parentOf, all, containerIds };
  });

  const pathOf = (n: N): Crumb[] => {
    const idx = index();
    const out: Crumb[] = [];
    let p = idx.parentOf.get(n.id);
    while (p) {
      out.unshift({ id: p.id, display: p.display });
      p = idx.parentOf.get(p.id);
    }
    return out;
  };

  // Deep link: when the route carries a focus id, open that node full-page.
  createEffect(() => {
    const f = cfg.focus?.();
    if (!f) {
      showFull(null);
      return;
    }
    showFull(index().byId.get(f) ?? null);
  });

  const filtering = createMemo(() => chips().length > 0);
  const flatten = createMemo(() => !!cfg.flat || filtering() || viewMode() === "list");
  const visible = createMemo(() => cfg.columnKeys.filter((k) => cols().includes(k)));

  const rows = createMemo<Row<N>[]>(() => {
    if (flatten()) {
      const pred = buildPredicate(cfg.filterKeys, chips());
      const list = index().all.filter(pred);
      const s = sort();
      list.sort((a, b) => {
        if (s) {
          const x = cfg.sortVal(a, s.key);
          const y = cfg.sortVal(b, s.key);
          const r = typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y));
          return r * s.dir;
        }
        return a.display.toLowerCase().localeCompare(b.display.toLowerCase());
      });
      return list.map((n) => ({ n, depth: 0, path: pathOf(n) }));
    }
    const out: Row<N>[] = [];
    const ex = expanded();
    const walk = (list: N[], depth: number) => {
      for (const n of list) {
        out.push({ n, depth, path: null });
        const kids = (n.children as N[]) ?? [];
        if (kids.length && ex.has(n.id)) walk(kids, depth + 1);
      }
    };
    walk(cfg.nodes(), 0);
    return out;
  });

  const toggleSort = (key: string) =>
    setSort((s) => (s?.key !== key ? { key, dir: 1 } : s.dir === 1 ? { key, dir: -1 } : null));
  const toggleNode = (id: string) =>
    setExpanded((s) => {
      const next = new Set(s);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  const allExpanded = createMemo(() => {
    const c = index().containerIds;
    return c.size > 0 && [...c].every((id) => expanded().has(id));
  });
  const toggleAll = () => setExpanded(allExpanded() ? new Set<string>() : new Set(index().containerIds));
  const toggleCol = (k: string) => setCols((c) => (c.includes(k) ? c.filter((x) => x !== k) : [...c, k]));

  const openNode = (n: N) => (cfg.onOpenNode ? cfg.onOpenNode(n) : showFull(n));
  const back = () => (cfg.onBack ? cfg.onBack() : showFull(null));

  const ctx: ListCtx<N> = {
    fact: (label, value) => (
      <div>
        <div class="eyebrow mb-1.5">{label}</div>
        <div class="text-sm">{value}</div>
      </div>
    ),
    field: (label, control, hint) => (
      <label class="flex flex-col gap-1.5">
        <span class="eyebrow">{label}</span>
        {control}
        <Show when={hint}>
          <span class="text-xs text-base-content/50">{hint}</span>
        </Show>
      </label>
    ),
    facetActive: (key, val) => facetActiveFn(chips(), key, val),
    toggleFacet: (key, val) => setChips(toggleFacetFn(chips(), key, val)),
    openEdit: (n) => setForm({ mode: "edit", node: n }),
    openCreate: (parent) => setForm({ mode: "create", parent }),
    openNode,
    setFullPage: (n) => showFull(n),
    pathOf,
    nav: cfg.nav,
  };

  const colBox = (on: boolean) =>
    "flex h-4 w-4 flex-none items-center justify-center rounded border " +
    (on ? "border-primary bg-primary text-primary-content" : "border-base-300");

  const actions = (
    <>
      <Show when={!cfg.flat}>
        <button
          class="btn btn-ghost btn-sm btn-square"
          title={viewMode() === "list" ? "Switch to tree view" : "Switch to list view"}
          onClick={() => setViewMode(viewMode() === "list" ? "tree" : "list")}
        >
          {viewMode() === "list" ? <ListTree size={15} /> : <Rows size={15} />}
        </button>
      </Show>
      <Show when={!flatten()}>
        <button class="btn btn-ghost btn-sm btn-square" title={allExpanded() ? "Collapse all" : "Expand all"} onClick={toggleAll}>
          {allExpanded() ? <ChevronsDownUp size={15} /> : <ChevronsUpDown size={15} />}
        </button>
      </Show>
      <details class="dropdown dropdown-end">
        <summary class="btn btn-ghost btn-sm btn-square" title="Columns">
          <Columns size={15} />
        </summary>
        <ul class="dropdown-content menu z-40 mt-1.5 w-52 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl">
          <li class="menu-title px-2 pb-1.5 text-[10.5px]">Columns</li>
          <For each={cfg.columnKeys}>
            {(k) => (
              <li>
                <button class="flex items-center gap-2.5 px-2 py-1.5" onClick={() => toggleCol(k)}>
                  <span class={colBox(cols().includes(k))}>
                    <Show when={cols().includes(k)}>
                      <Check size={11} />
                    </Show>
                  </span>
                  {cfg.columns[k].label}
                </button>
              </li>
            )}
          </For>
        </ul>
      </details>
      <span class="mx-1 h-5 w-px flex-none bg-base-300" />
      <Show when={allow("create")}>
        <button class="btn btn-primary btn-sm" onClick={() => ctx.openCreate(null)}>
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
        class="sticky top-0 z-[5] select-none bg-base-200 text-left"
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
      <tr class="group cursor-pointer hover:bg-base-content/5" onClick={() => openNode(n)}>
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
          {(k) => <td class="overflow-hidden text-ellipsis whitespace-nowrap text-sm">{cfg.cellFor(k, n, ctx)}</td>}
        </For>
        <td>
          <div class="flex justify-end gap-0.5 opacity-0 transition group-hover:opacity-100">
            <button class="btn btn-ghost btn-xs btn-square" title="Open full page" onClick={(e) => { e.stopPropagation(); showFull(n); }}>
              <Maximize size={15} />
            </button>
            <Show when={cfg.canAddChild?.(n) && allow("create")}>
              <button class="btn btn-ghost btn-xs btn-square" title="Add child" onClick={(e) => { e.stopPropagation(); ctx.openCreate(n); }}>
                <Plus size={15} />
              </button>
            </Show>
            <Show when={allow("update")}>
              <button class="btn btn-ghost btn-xs btn-square" title="Edit" onClick={(e) => { e.stopPropagation(); ctx.openEdit(n); }}>
                <Pencil size={15} />
              </button>
            </Show>
            <Show when={allow("delete") && cfg.onDelete}>
              <button class="btn btn-ghost btn-xs btn-square text-error" title="Delete" onClick={(e) => { e.stopPropagation(); cfg.onDelete!(n, ctx); }}>
                <Trash size={15} />
              </button>
            </Show>
          </div>
        </td>
      </tr>
    );
  };

  const ListBody = () => (
    <section class="fade-in flex flex-col gap-3.5">
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
                <th class="sticky top-0 z-[5] bg-base-200" />
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
      <button class="btn btn-ghost btn-sm flex-none gap-1.5 self-start" onClick={back}>
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
                      if (anc) openNode(anc);
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
      <div class="card border border-base-300 bg-base-200 og-pad">{cfg.renderDetail(props2.node, ctx)}</div>
    </section>
  );

  return (
    <>
      <Show when={cfg.error?.()}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm">
          <span>Could not load {cfg.entity.plural.toLowerCase()}: {String(cfg.error?.())}</span>
        </div>
      </Show>
      <Show when={fullPage()} fallback={<ListBody />}>
        {(n) => <FullPage node={n()} />}
      </Show>
      <Show when={form()}>
        {(f) => (
          <Drawer open={true} onClose={() => setForm(null)} title={f().mode === "edit" ? `Edit ${cfg.entity.name}` : `New ${cfg.entity.name}`}>
            <Dynamic component={cfg.FormBody} form={f()} close={() => setForm(null)} ctx={ctx} />
          </Drawer>
        )}
      </Show>
    </>
  );
}
