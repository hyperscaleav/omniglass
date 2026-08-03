import { createEffect, createSignal, onCleanup, type Accessor } from "solid-js";
import { createStore, reconcile } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";
import { watchView } from "./viewwatch";

// The client half of the views read side. One shape (`ViewResult`) and one data
// primitive (`useView`) serve every read surface: a page picks a view by name,
// a renderer reads cells through the view's field-mapping, and neither ever
// builds a request or holds a socket. The server contract is
// docs/architecture/views.

export type ViewCell = string | number | boolean | null;

export type ViewColumn = {
  name: string;
  type: string;
  role?: string;
};

export type ViewResult = {
  columns: ViewColumn[];
  rows: ViewCell[][];
  next_page_token?: string;
};

// A field-mapping names, per renderer role (value, label, time, series), which
// column carries it. It is published per view in the directory, so a renderer
// never hardcodes a column name.
export type FieldMapping = Record<string, string>;

// columnIndex resolves a column name to its position in a row, or -1.
export function columnIndex(result: ViewResult, name: string): number {
  return result.columns.findIndex((c) => c.name === name);
}

// requireRole resolves a renderer role to its cell position through the
// field-mapping, and THROWS when the mapping does not lead to a real column.
// Loud on purpose: a renamed column would otherwise render a column of blanks,
// which reads as "no data" and hides a broken contract until someone notices
// the estate looks empty.
export function requireRole(result: ViewResult, mapping: FieldMapping, role: string): number {
  const name = mapping[role];
  if (!name) {
    throw new Error(`view field-mapping has no column for the ${role} role`);
  }
  const at = columnIndex(result, name);
  if (at < 0) {
    throw new Error(`view field-mapping maps ${role} to ${name}, which the result does not carry`);
  }
  return at;
}

// viewKey is the query key for one view run. Params are sorted so the same
// request keys identically however a caller ordered them, which keeps a
// re-render from missing the cache and refetching.
export function viewKey(name: string, params?: string[]) {
  return ["view", name, [...(params ?? [])].sort()] as const;
}

// runView executes one view through the generated typed client.
async function runView(name: string, params: string[], pageToken?: string): Promise<ViewResult> {
  const { data, error } = await api.GET("/views/{name}:run", {
    params: {
      path: { name },
      query: { param: params, ...(pageToken ? { page_token: pageToken } : {}) },
    },
  });
  if (error) throw error;
  return data as ViewResult;
}

export type UseViewOptions = {
  // watch subscribes to the view's change stream, so the query invalidates on
  // a server-side change instead of on a timer. Default true.
  watch?: boolean;
  // refetchIntervalMs is the fallback cadence used when watching is off, for a
  // surface that wants periodic freshness without a stream.
  refetchIntervalMs?: number;
  // pageToken runs a specific page rather than the first.
  pageToken?: string;
};

// useView is the ONE data primitive for reading a view: solid-query owns fetch,
// cache, dedup, and retry, and the watch stream drives invalidation for this
// view's key. Pages and renderers never fetch, so caching and liveness are
// decided in one place rather than per surface.
//
// params is an accessor, so a page whose parameters are reactive (a selected
// component, a chosen window) re-keys and refetches without any imperative
// wiring.
export function useView(name: string, params?: Accessor<string[]>, options: UseViewOptions = {}) {
  const qc = useQueryClient();
  const currentParams = () => params?.() ?? [];

  const query = useQuery(() => ({
    queryKey: viewKey(name, currentParams()),
    queryFn: () => runView(name, currentParams(), options.pageToken),
    // A watched view is refreshed by its stream; only an unwatched one polls.
    refetchInterval: options.watch === false ? options.refetchIntervalMs : undefined,
  }));

  createEffect(() => {
    if (options.watch === false) return;
    // Re-subscribing on a params change keeps the stream and the query on the
    // same request: a watcher for the old params would invalidate a key
    // nothing is reading.
    const p = currentParams();
    const handle = watchView(name, p, () => {
      void qc.invalidateQueries({ queryKey: viewKey(name, p) });
    });
    onCleanup(() => handle.close());
  });

  return query;
}

// useViewRows applies a view's rows through a store with `reconcile`, which is
// what makes a live surface cheap: a one-row delta patches that row's cells in
// place, so every other row keeps its identity and its DOM, and only the cells
// that actually changed re-render. Re-assigning the array instead would rebuild
// the whole table on every notification.
// ViewRow wraps a result row for the store. The wrapper exists for one reason:
// reconcile cannot merge bare arrays in place (it replaces them, and every row
// would lose its identity and its DOM on any update), while it merges keyed
// OBJECTS cell by cell. The key is the row's position, which is the only
// identity a positional ViewResult carries; a view that later declares a row
// key can key on that instead and survive re-ordering too.
export type ViewRow = { k: string; cells: ViewCell[] };

export function useViewRows(result: Accessor<ViewResult | undefined>): {
  rows: Accessor<ViewRow[]>;
  columns: Accessor<ViewColumn[]>;
} {
  const [store, setStore] = createStore<{ rows: ViewRow[] }>({ rows: [] });
  const [columns, setColumns] = createSignal<ViewColumn[]>([]);
  createEffect(() => {
    const r = result();
    const next: ViewRow[] = (r?.rows ?? []).map((cells, i) => ({ k: String(i), cells }));
    setStore("rows", reconcile(next, { key: "k" }));
    setColumns(r?.columns ?? []);
  });
  // An ACCESSOR, not the array itself: reading store.rows here would resolve it
  // once, outside any tracking scope, and every consumer would then hold a
  // stale snapshot that never updates.
  return { rows: () => store.rows, columns };
}
