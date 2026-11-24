import { type Accessor, type JSX, For, Show, createEffect, createMemo, createSignal, untrack } from "solid-js";
import ListShell from "./ListShell";
import Drawer from "./Drawer";
import BladeStack from "./BladeStack";
import { type BladeController, type BladeDef, BladesContext, createBladeController } from "../lib/blades";
import { ChevronDown, ChevronRight, Plus } from "./icons";
import Button from "./Button";
import { useDrawerAddress } from "../lib/drawer_address";
import { resolveFilterKeys, type Chip, type FilterKeys } from "../lib/predicate";

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

// wide asks for the Drawer's wide panel, for a body that reads more than a
// form (the audit trail's before/after change, #1088).
export type FlatDetail = { title: JSX.Element; body: JSX.Element; wide?: boolean };

export type FlatConfig<T> = {
  // entity.name is the singular display noun (the create Drawer title reads
  // "New <name>"); plural labels the empty/error copy. Both are UI text, not the
  // authorization resource (auth is gated explicitly on the create config's `can`).
  entity: { name: string; plural: string };
  rows: Accessor<T[]>;
  loading?: Accessor<boolean>;
  // Opt in to holding the table's space while `loading`: this many skeleton rows
  // replace the "Loading…" line, so the loaded rows land in place instead of
  // shifting what sits below the list (the probe's layout-shift budget).
  loadingRows?: number;
  // Lay the table out with fixed column widths (table-layout: fixed), so cell
  // content never sizes a column. For a list whose cells vary run to run or are
  // masked in a screenshot (the audit trail); columns with no width split the rest.
  fixedLayout?: boolean;
  error?: Accessor<unknown>;
  filterKeys: FilterKeys<T>;
  filterPlaceholder?: string;
  initialChips?: Chip[];
  columns: FlatColumn<T>[];
  // No rows at all: a line, or a teaching face (a function, given the create
  // action when the viewer may create) that replaces the line, as Inbox does.
  empty?: string | ((ctx: { create?: () => void }) => JSX.Element);
  rowClass?: (r: T) => string;
  // The name a row's open control announces ("Open <name>"); absent, the entity's
  // singular noun and the row's address (rowId) stand in, so rows still differ.
  rowLabel?: (r: T) => string;
  // A row opens this side Drawer detail. Omit for a read-only table (the audit log).
  detail?: (r: T) => FlatDetail;
  // The primary create action: a rail button that opens a Drawer with `body`. The
  // body receives a small context: `close` dismisses the create Drawer; `select`
  // opens a row's detail Drawer (closing create), so a successful create can land
  // the operator straight on the new row.
  // openInitially opens the create Drawer on first render: a link that carries a
  // new-row intent (an "Alarm on this" that arrives with the form's fields filled).
  create?: { label: string; can: () => boolean; openInitially?: boolean; body: (ctx: { close: () => void; select: (row: T) => void }) => JSX.Element };
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
  // A second address a row answers to (its uuid, where rowId is its name), so a link
  // minted before a rename still lands on it (ADR-0062: the route takes either).
  rowAlt?: (r: T) => string | undefined;
  openId?: () => string | undefined;
  // An older bespoke id param (`u`, `g`, `n`) this page's links used before the
  // drawer address (`?open=`, #2429): still read on load, swept on the next write.
  legacyParam?: string;
  // When present, a row (and openId) opens a cross-entity blade stack instead of
  // the single Drawer detail. `rootKind` is the kind a row opens; `catalog` maps
  // each kind to its blade renderers; a body drills via useBlades(). Requires rowId.
  // The blade stack and the Drawer detail are mutually exclusive; a config sets one.
  // `rowKind` maps a ROW to its kind for a mixed-kind surface; absent, every row
  // opens under `rootKind`, exactly the single-kind behavior every entity page
  // relies on. No shipped page maps rowKind today (the merged Catalog browse that
  // motivated it was retired for the shell); the contract stays pinned by
  // FlatList.test for the next mixed-kind surface.
  blades?: { registry: Record<string, BladeDef>; rootKind: string; rowKind?: (r: T) => string; controller?: BladeController };
  // Controlled chip state (optional, forwarded to ListShell): a page whose own
  // controls construct chips owns the signal and passes both; omit both for the
  // shell-owned default every shipped page uses today (no page passes these yet).
  chips?: Accessor<Chip[]>;
  onChips?: (chips: Chip[]) => void;
  // Fill mode (optional, forwarded to ListShell): for a page that caps its own
  // height, the table region takes the surplus and scrolls internally under
  // sticky column headers instead of growing the page (the audit trail, whose
  // first page outgrows its skeleton). Off, the list grows with content.
  fill?: boolean;
};

type SortState = { key: string; dir: 1 | -1 } | null;

export default function FlatList<T>(props: { config: FlatConfig<T> }) {
  const cfg = props.config;
  const [sort, setSort] = createSignal<SortState>(null);
  const [selected, setSelected] = createSignal<T | null>(null);
  const [createOpen, setCreateOpen] = createSignal(!!cfg.create?.openInitially && cfg.create.can());

  // A row opens either a blade (cross-entity stack) or the single Drawer detail.
  // With a rowId the open row is the URL's `?open=` address (lib/drawer_address):
  // opening pushes it, closing clears it, Back closes it, and a loaded address
  // opens the same drawer. Without a rowId (the audit log) the drawer is local.
  const blades = cfg.blades?.controller ?? createBladeController();
  const openName = (r: T) => cfg.rowLabel?.(r) ?? (cfg.rowId ? `${cfg.entity.name} ${cfg.rowId(r)}` : cfg.entity.name);
  const rowOf = (id: string) => (cfg.rowId ? cfg.rows().find((r) => cfg.rowId!(r) === id || cfg.rowAlt?.(r) === id) : undefined);
  // The row object a click or create just handed over, so a drawer opens before
  // the refetch that will list it (a freshly created row) lands.
  const [handed, setHanded] = createSignal<{ id: string; row: T } | null>(null);
  createEffect(() => {
    const h = handed();
    if (h && rowOf(h.id)) setHanded(null);
  });
  const exists = (id: string): boolean | undefined => {
    if (rowOf(id) || handed()?.id === id) return true;
    return cfg.loading?.() ? undefined : false;
  };
  const addressed = !!cfg.rowId && (!!cfg.detail || !!cfg.blades);
  const addr = useDrawerAddress({
    exists,
    legacy: cfg.legacyParam,
    blades: cfg.blades ? blades : undefined,
    rootRef: (id) => {
      const row = rowOf(id) ?? handed()?.row;
      return { kind: (row && cfg.blades?.rowKind?.(row)) ?? cfg.blades?.rootKind ?? "", id };
    },
  });
  const openRow = (r: T) => {
    if (addressed) {
      const id = cfg.rowId!(r);
      setHanded({ id, row: r });
      addr.open(id);
    } else setSelected(() => r);
  };
  const detailRow = createMemo<T | null>(() => {
    if (!cfg.detail) return null;
    if (!addressed) return selected();
    const id = addr.address();
    return id ? (rowOf(id) ?? (handed()?.id === id ? handed()!.row : null)) : null;
  });
  // The drawer mounts once per opened row (keyed on the address, not the row
  // object), so a background refetch never remounts a body mid-edit.
  const detailKey = createMemo(() => {
    const r = detailRow();
    return r ? (addressed ? addr.address()! : "local") : undefined;
  });

  // Programmatic deep link (openId): opens the row once it is loaded. Guarded by the
  // last-opened id so closing the drawer does not immediately re-open it.
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
        <Button intent="action" icon={Plus} onClick={() => setCreateOpen(true)}>{cfg.create!.label}</Button>
      </Show>
    </>
  );

  const ariaSort = (key: string) => (sort()?.key !== key ? "none" : sort()!.dir === 1 ? "ascending" : "descending");
  const Th = (p: { col: FlatColumn<T> }) => {
    const label = (
      <span class="inline-flex items-center gap-1">
        {p.col.label}
        <Show when={sort()?.key === p.col.key}>
          <span class="inline-flex text-primary" style={{ transform: sort()!.dir === -1 ? "rotate(180deg)" : undefined }}>
            <ChevronDown size={13} />
          </span>
        </Show>
      </span>
    );
    return (
      <th class={p.col.headClass ?? ""} aria-sort={p.col.sortVal ? ariaSort(p.col.key) : undefined}>
        <Show when={p.col.sortVal} fallback={label}>
          <button type="button" class="cursor-pointer select-none [font:inherit] [letter-spacing:inherit] [text-transform:inherit]" onClick={() => toggleSort(p.col.key)}>
            {label}
          </button>
        </Show>
      </th>
    );
  };

  return (
    <BladesContext.Provider value={blades}>
      <ListShell
        filterKeys={resolveFilterKeys(cfg.filterKeys)}
        rows={cfg.rows()}
        placeholder={cfg.filterPlaceholder}
        initialChips={cfg.initialChips}
        chips={cfg.chips}
        onChips={cfg.onChips}
        fill={cfg.fill}
        counted
        error={cfg.error?.()}
        errorLabel={`Could not load ${cfg.entity.plural.toLowerCase()}`}
        trailing={trailing}
        missing={addressed && addr.miss() ? { noun: cfg.entity.name, address: addr.miss()!, onDismiss: addr.close } : undefined}
      >
        {(filtered, chips) => {
          const shown = createMemo(() => applySort(filtered()));
          const filtering = () => chips().length > 0;
          const openable = !!cfg.detail || !!cfg.blades;
          const span = () => cfg.columns.length + (openable ? 1 : 0);
          return (
            <>
              <div
                class="overflow-x-auto"
                classList={{
                  // Fill mode: this region absorbs the card's surplus height and
                  // scrolls; the headers stick to its top (bg matches the card so
                  // rows pass beneath them cleanly).
                  "min-h-0 flex-1 overflow-y-auto [&_thead_th]:sticky [&_thead_th]:top-0 [&_thead_th]:z-10 [&_thead_th]:bg-base-200": cfg.fill,
                }}
              >
                <table class={cfg.fixedLayout ? "table table-sm table-fixed w-full" : "table table-sm"}>
                  <Show when={cfg.columns.some((c) => c.width)}>
                    <colgroup>
                      <For each={cfg.columns}>{(c) => <col style={c.width ? { width: c.width } : undefined} />}</For>
                      <Show when={openable}><col style={{ width: "40px" }} /></Show>
                    </colgroup>
                  </Show>
                  {/* A list with nothing in it has no columns to head: the empty state
                      stands alone (#2431). Loading keeps the head (the rows land
                      under it), and a filter that matches nothing keeps it too. */}
                  <Show when={cfg.loading?.() || cfg.rows().length > 0}>
                  <thead>
                    <tr>
                      <For each={cfg.columns}>{(c) => <Th col={c} />}</For>
                      <Show when={openable}><th /></Show>
                    </tr>
                  </thead>
                  </Show>
                  <tbody>
                    <For
                      each={shown()}
                      fallback={
                        <Show
                          when={cfg.loading?.() && cfg.loadingRows}
                          fallback={
                            <tr>
                              <Show
                                when={!cfg.loading?.() && !filtering() && typeof cfg.empty === "function"}
                                fallback={
                                  <td colspan={span()} class="py-8 text-center text-base-content/40">
                                    {cfg.loading?.() ? "Loading…" : filtering() ? `No ${cfg.entity.plural.toLowerCase()} match the filter.` : ((cfg.empty as string | undefined) ?? `No ${cfg.entity.plural.toLowerCase()} yet.`)}
                                  </td>
                                }
                              >
                                <td colspan={span()} class="p-0" data-testid="list-empty">
                                  {(cfg.empty as (ctx: { create?: () => void }) => JSX.Element)({ create: cfg.create?.can() ? () => setCreateOpen(true) : undefined })}
                                </td>
                              </Show>
                            </tr>
                          }
                        >
                          <For each={Array.from({ length: cfg.loadingRows! })}>
                            {() => (
                              <tr data-testid="list-loading-row" aria-hidden="true">
                                <td colspan={span()}><div class="skeleton h-5 w-full" /></td>
                              </tr>
                            )}
                          </For>
                        </Show>
                      }
                    >
                      {(r) => (
                        <tr
                          class={`border-base-200 ${cfg.rowClass?.(r) ?? ""} ${openable ? "cursor-pointer hover:bg-base-100" : ""}`}
                          onClick={openable ? () => openRow(r) : undefined}
                        >
                          <For each={cfg.columns}>{(c) => <td>{c.cell(r)}</td>}</For>
                          <Show when={openable}>
                            <td class="text-base-content/30">
                              {/* The keyboard stop for the row: Enter/Space clicks it and the click bubbles to the row's onClick, so no second handler. */}
                              <button type="button" class="inline-flex cursor-pointer" aria-label={`Open ${openName(r)}`}>
                                <ChevronRight size={15} />
                              </button>
                            </td>
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

      <Show when={detailKey()} keyed>
        {(_) => {
          const d = cfg.detail!(untrack(detailRow)!);
          return (
            <Drawer open={true} onClose={() => (addressed ? addr.close() : setSelected(null))} title={d.title} wide={d.wide}>
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
