import { createEffect, createSignal, onCleanup, type Accessor } from "solid-js";
import { createStore, reconcile } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { watchView } from "./viewwatch";

// The client half of the views read side. One shape (`ViewResult`) and one data
// primitive (`useView`) serve every read surface: a page picks a view by name,
// a renderer reads cells through the view's field-mapping, and neither ever
// builds a request or holds a socket. The server contract is
// docs/architecture/views.

// The shapes come from the generated schema, not from a hand-copy: the server
// owns the contract and `make gen` carries it here, so a column or a field
// added there cannot silently disagree with what the renderers read.
export type ViewColumn = NonNullable<components["schemas"]["ViewResult"]["columns"]>[number];
export type ViewResult = components["schemas"]["ViewResult"];

// ViewCell narrows what a cell can actually hold. The generated type says
// `unknown` because a row is a heterogeneous tuple; the server only ever emits
// these four, and every renderer is written against them.
export type ViewCell = string | number | boolean | null;

// A field-mapping names, per renderer role (value, label, time, series), which
// column carries it. It is published per view in the directory, so a renderer
// never hardcodes a column name.
export type FieldMapping = Record<string, string>;

// columnIndex resolves a column name to its position in a row, or -1.
export function columnIndex(result: ViewResult, name: string): number {
  return (result.columns ?? []).findIndex((c) => c.name === name);
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

// viewKey is the query key for one view run. Params are sorted because the
// server treats the bindings as a SET (a duplicate binding is a 400), so two
// callers that order the same bindings differently are asking one question and
// should share one cache entry.
export function viewKey(name: string, params?: string[], pageToken?: string) {
  // The page token is PART of the key. Two surfaces reading different pages of
  // one view are two different results; sharing a cache entry would let one
  // overwrite the other's rows, and a watch invalidation would then replay
  // whichever page happened to win.
  return ["view", name, [...(params ?? [])].sort(), pageToken ?? ""] as const;
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
  // refetchIntervalMs is the fallback cadence used when watching is off. With
  // watch off and no interval the view is fetched once and never refreshed,
  // which is the right shape for a one-shot read and the wrong one for a live
  // surface, so a caller turning watch off should say which it wants.
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
    queryKey: viewKey(name, currentParams(), options.pageToken),
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
    const subscribedAt = Date.now();
    let baselineSeen = false;
    const release = shareWatch(name, p, () => {
      // The server opens every stream with a baseline change event, so a
      // reconnecting client always refetches once. When the query already
      // holds data newer than this connection, that baseline says nothing new,
      // and acting on it would double-fetch every view on mount and again on
      // every stream renewal.
      if (!baselineSeen) {
        baselineSeen = true;
        const state = qc.getQueryState(viewKey(name, p, options.pageToken));
        if (state?.dataUpdatedAt && state.dataUpdatedAt >= subscribedAt) return;
      }
      void qc.invalidateQueries({ queryKey: viewKey(name, p, options.pageToken) });
    });
    onCleanup(release);
  });

  return query;
}

// The shared watch registry. A browser allows only a handful of concurrent
// connections per origin over HTTP/1.1, and a stream holds one open for its
// whole life, so a dashboard of widgets that each opened their own would starve
// every ordinary request on the page. Watchers of the same view with the same
// params share one stream; the last one to leave closes it.
type sharedWatch = { close: () => void; listeners: Set<() => void> };
const sharedWatches = new Map<string, sharedWatch>();

function shareWatch(name: string, params: string[], onChange: () => void): () => void {
  const key = JSON.stringify(viewKey(name, params));
  let entry = sharedWatches.get(key);
  if (!entry) {
    const listeners = new Set<() => void>();
    const handle = watchView(name, params, () => {
      for (const fn of [...listeners]) fn();
    });
    entry = { close: handle.close, listeners };
    sharedWatches.set(key, entry);
  }
  const shared = entry;
  shared.listeners.add(onChange);
  return () => {
    shared.listeners.delete(onChange);
    if (shared.listeners.size === 0) {
      shared.close();
      sharedWatches.delete(key);
    }
  };
}

// useViewRows applies a view's rows through a store with `reconcile`, so a
// live update patches cells in place instead of rebuilding the table.
//
// The guarantee, stated honestly: rows keep their DOM nodes across ANY update,
// and only the cells whose values actually changed re-render. For a view that
// updates rows in place (component-reachability flipping one verdict) that is
// one cell. For a view that PREPENDS (event-feed, newest first) it is every
// cell, because the rows are keyed by POSITION, the only identity a positional
// ViewResult carries: row 0 now holds what row 1 held. The nodes survive, the
// values shift down. A view that later declares a row key can key on that and
// survive insertion and re-ordering too.
export type ViewRow = { k: string; cells: ViewCell[] };

export function useViewRows(result: Accessor<ViewResult | undefined>): {
  rows: Accessor<ViewRow[]>;
  columns: Accessor<ViewColumn[]>;
} {
  const [store, setStore] = createStore<{ rows: ViewRow[] }>({ rows: [] });
  const [columns, setColumns] = createSignal<ViewColumn[]>([]);
  createEffect(() => {
    const r = result();
    // A null rows array and a null row are both "no rows" on the wire; the
    // renderers see one shape.
    const next: ViewRow[] = (r?.rows ?? []).map((cells, i) => ({ k: String(i), cells: (cells ?? []) as ViewCell[] }));
    setStore("rows", reconcile(next, { key: "k" }));
    setColumns(r?.columns ?? []);
  });
  // An ACCESSOR, not the array itself: reading store.rows here would resolve it
  // once, outside any tracking scope, and every consumer would then hold a
  // stale snapshot that never updates.
  return { rows: () => store.rows, columns };
}
