import { type Accessor, type JSX, For, Show, createEffect, createMemo, createSignal } from "solid-js";
import ListShell from "./ListShell";
import Drawer from "./Drawer";
import BladeStack from "./BladeStack";
import { type BladeController, type BladeDef, BladesContext, createBladeController } from "../lib/blades";
import { ChevronDown, ChevronRight, Plus } from "./icons";
import type { Chip, FilterKey } from "../lib/predicate";

// FlatList is the body for a flat (non-tree) list surface: a sortable table over
// the ListShell-filtered rows, with an optional row -> side Drawer detail, an
// optional create Drawer, and an optional footer (e.g. the audit trail's load
// older). It is the flat sibling of TreeList; both wear ListShell's chrome, and
// each owns only its own detail idiom (a single drawer here, a blade stack there).

export type FlatColumn<T> = {
  key: string;
  label: string;
  cell: (r: T) => JSX.Element;
  // When present the column header sorts; absent means the column is not sortable.
  sortVal?: (r: T) => string | number;
  width?: string; // a CSS width for the colgroup, e.g. "180px"
  headClass?: string;
};

export type FlatDetail = { title: JSX.Element; body: JSX.Element };

export type FlatConfig<T> = {
  // entity.name is the authorization resource; plural labels empty/error copy.
  entity: { name: string; plural: string };
  rows: Accessor<T[]>;
  loading?: Accessor<boolean>;
  error?: Accessor<unknown>;
  filterKeys: FilterKey<T>[];
  filterPlaceholder?: string;
  initialChips?: Chip[];
  columns: FlatColumn<T>[];
  empty?: string; // no rows at all
  rowClass?: (r: T) => string;
  // A row opens this side Drawer detail. Omit for a read-only table (the audit log).
  detail?: (r: T) => FlatDetail;
  // The primary create action: a rail button that opens a Drawer with `body`. The
  // body receives a small context: `close` dismisses the create Drawer; `select`
  // opens a row's detail Drawer (closing create), so a successful create can land
  // the operator straight on the new row.
  create?: { label: string; can: () => boolean; body: (ctx: { close: () => void; select: (row: T) => void }) => JSX.Element };
  // An extra control in the action rail, left of the create button (e.g. a "show
  // archived" toggle on the Users directory).
  railExtra?: () => JSX.Element;
  // A trailing row under the table (counts, load-older); receives the shown/total
  // counts and whether a filter is active.
  footer?: (info: { shown: number; total: number; filtering: boolean }) => JSX.Element;
  // Open a row's detail by id (a stable key), so another surface can deep-link to
  // it (e.g. clicking a group on a user's detail opens that group here). `rowId`
  // supplies the key; when `openId` changes to a new value whose row is loaded, its
  // detail Drawer opens once (closing it does not re-open, until openId changes).
  rowId?: (r: T) => string;
  openId?: () => string | undefined;
  // When present, a row (and openId) opens a cross-entity blade stack instead of
  // the single Drawer detail. `rootKind` is the kind a row opens; `registry` maps
  // each kind to its blade renderers; a body drills via useBlades(). Requires rowId.
  // The blade stack and the Drawer detail are mutually exclusive; a config sets one.
  blades?: { registry: Record<string, BladeDef>; rootKind: string; controller?: BladeController };
};

type SortState = { key: string; dir: 1 | -1 } | null;

export default function FlatList<T>(props: { config: FlatConfig<T> }) {
  const cfg = props.config;
  const [sort, setSort] = createSignal<SortState>(null);
  const [selected, setSelected] = createSignal<T | null>(null);
  const [createOpen, setCreateOpen] = createSignal(false);

  // A row opens either a blade (cross-entity stack) or the single Drawer detail.
  const blades = cfg.blades?.controller ?? createBladeController();
  const openRow = (r: T) => {
    if (cfg.blades && cfg.rowId) blades.push({ kind: cfg.blades.rootKind, id: cfg.rowId(r) });
    else setSelected(() => r);
  };

  // Deep-link: when openId names a loaded row, open its detail once. Guarded by the
  // last-opened id so closing the Drawer does not immediately re-open it.
  const [openedId, setOpenedId] = createSignal<string | undefined>();
  createEffect(() => {
    const id = cfg.openId?.();
    if (!id || id === openedId() || !cfg.rowId) return;
    const row = cfg.rows().find((r) => cfg.rowId!(r) === id);
    if (row) {
      openRow(row);
      setOpenedId(id);
    }
  });

  const colByKey = (key: string) => cfg.columns.find((c) => c.key === key);
  const toggleSort = (key: string) => {
    if (!colByKey(key)?.sortVal) return;
    setSort((s) => (s?.key !== key ? { key, dir: 1 } : s.dir === 1 ? { key, dir: -1 } : null));
  };
  const applySort = (rows: T[]): T[] => {
    const s = sort();
    const col = s && colByKey(s.key);
    if (!s || !col?.sortVal) return rows;
    const get = col.sortVal;
    return [...rows].sort((a, b) => {
      const av = get(a);
      const bv = get(b);
      return av < bv ? -s.dir : av > bv ? s.dir : 0;
    });
  };

  const trailing = (
    <>
      {cfg.railExtra?.()}
      <Show when={cfg.create?.can()}>
        <button class="btn btn-action btn-sm gap-1.5" onClick={() => setCreateOpen(true)}>
          <Plus size={14} /> {cfg.create!.label}
        </button>
      </Show>
    </>
  );

  const Th = (p: { col: FlatColumn<T> }) => (
    <th
      class={`${p.col.headClass ?? ""} ${p.col.sortVal ? "cursor-pointer select-none" : ""}`}
      onClick={() => toggleSort(p.col.key)}
    >
      <span class="inline-flex items-center gap-1">
        {p.col.label}
        <Show when={sort()?.key === p.col.key}>
          <span class="inline-flex text-primary" style={{ transform: sort()!.dir === -1 ? "rotate(180deg)" : undefined }}>
            <ChevronDown size={13} />
          </span>
        </Show>
      </span>
    </th>
  );

  return (
    <BladesContext.Provider value={blades}>
      <ListShell
        filterKeys={cfg.filterKeys}
        rows={cfg.rows()}
        placeholder={cfg.filterPlaceholder}
        initialChips={cfg.initialChips}
        error={cfg.error?.()}
        errorLabel={`Could not load ${cfg.entity.plural.toLowerCase()}`}
        trailing={trailing}
      >
        {(filtered, chips) => {
          const shown = createMemo(() => applySort(filtered()));
          const filtering = () => chips().length > 0;
          const openable = !!cfg.detail || !!cfg.blades;
          const span = () => cfg.columns.length + (openable ? 1 : 0);
          return (
            <>
              <div class="overflow-x-auto">
                <table class="table table-sm">
                  <Show when={cfg.columns.some((c) => c.width)}>
                    <colgroup>
                      <For each={cfg.columns}>{(c) => <col style={c.width ? { width: c.width } : undefined} />}</For>
                      <Show when={openable}><col style={{ width: "40px" }} /></Show>
                    </colgroup>
                  </Show>
                  <thead>
                    <tr>
                      <For each={cfg.columns}>{(c) => <Th col={c} />}</For>
                      <Show when={openable}><th /></Show>
                    </tr>
                  </thead>
                  <tbody>
                    <For
                      each={shown()}
                      fallback={
                        <tr>
                          <td colspan={span()} class="py-8 text-center text-base-content/40">
                            {cfg.loading?.() ? "Loading…" : filtering() ? `No ${cfg.entity.plural.toLowerCase()} match the filter.` : (cfg.empty ?? `No ${cfg.entity.plural.toLowerCase()} yet.`)}
                          </td>
                        </tr>
                      }
                    >
                      {(r) => (
                        <tr
                          class={`border-base-200 ${cfg.rowClass?.(r) ?? ""} ${openable ? "cursor-pointer hover:bg-base-100" : ""}`}
                          onClick={openable ? () => openRow(r) : undefined}
                        >
                          <For each={cfg.columns}>{(c) => <td>{c.cell(r)}</td>}</For>
                          <Show when={openable}>
                            <td class="text-base-content/30"><ChevronRight size={15} /></td>
                          </Show>
                        </tr>
                      )}
                    </For>
                  </tbody>
                </table>
              </div>
              <Show when={cfg.footer}>
                <div class="flex items-center justify-between border-t border-base-300 px-3 py-2.5 text-xs text-base-content/50">
                  {cfg.footer!({ shown: shown().length, total: cfg.rows().length, filtering: filtering() })}
                </div>
              </Show>
            </>
          );
        }}
      </ListShell>

      <Show when={cfg.detail && selected()}>
        {(_) => {
          const d = cfg.detail!(selected()!);
          return (
            <Drawer open={true} onClose={() => setSelected(null)} title={d.title}>
              {d.body}
            </Drawer>
          );
        }}
      </Show>

      <Show when={cfg.create && createOpen()}>
        <Drawer open={true} onClose={() => setCreateOpen(false)} title={`New ${cfg.entity.name}`}>
          {cfg.create!.body({
            close: () => setCreateOpen(false),
            select: (row) => {
              openRow(row);
              setCreateOpen(false);
            },
          })}
        </Drawer>
      </Show>

      <Show when={cfg.blades}>
        <BladeStack controller={blades} registry={cfg.blades!.registry} />
      </Show>
    </BladesContext.Provider>
  );
}
