// The views binding (#1830, ADR-1830): one scoped read a surface binds to,
// its history from /views:run and its live rows from /views:watch. A page
// binds as many as it draws, and every binding on it rides the tab's one
// stream (lib/live.ts): the hub collects them, opens /views:watch with each
// binding's cursor, and hands each its own rows. History and live meet at the
// cursor the run answered, so a fold sees every row exactly once.
import { createEffect, createMemo, createSignal, onCleanup } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";
import type { ViewBindingBody, ViewResultBody, ViewTailBindingBody } from "../api/api.gen";
import type { ChartBucket } from "./chart";
import { ViewHub, bindingKey, viewHub } from "./live";

// The stream's plumbing lives in lib/live.ts; these names stay importable
// from here for the surfaces that bind.
export { ViewHub, VIEW_MAX_BINDINGS, bindingKey, laterCursor, sseParser, viewHub } from "./live";
export type { SseEvent, StreamOpener, ViewListener } from "./live";

export type ViewBinding = ViewBindingBody;
export type ViewResult = ViewResultBody;
// ViewTail is one binding's share of a rows event: the rows landed for it.
export type ViewTail = ViewTailBindingBody;

export const viewRunKey = (b: ViewBinding | undefined) => ["view-run", b ? bindingKey(b) : null] as const;

export async function runView(b: ViewBinding): Promise<ViewResult> {
  const res = await api.POST("/views:run", { body: { bindings: [b] } });
  if (res.error) throw res.error;
  const result = res.data?.results?.[0];
  if (!result) throw new Error("views run answered no result");
  return result;
}

// createViewBinding binds a surface to a view: it reads the binding's history
// (shared with every surface binding the same read), then folds each live
// row into the state init made of it. A binding of undefined reads nothing.
// A reset re-reads the history and starts the fold again from it.
export function createViewBinding<T>(o: {
  binding: () => ViewBinding | undefined;
  init: (r: ViewResult) => T;
  fold: (state: T, rows: ViewTail, now: number) => T;
  hub?: ViewHub;
}) {
  const qc = useQueryClient();
  // The same read rebuilt (a polled parent's new object) is the same binding:
  // its fold carries on rather than starting over from the history.
  const binding = createMemo(o.binding, undefined, {
    equals: (a, b) => (a && bindingKey(a)) === (b && bindingKey(b)),
  });
  const history = useQuery(() => ({
    queryKey: viewRunKey(binding()),
    queryFn: () => runView(binding()!),
    enabled: !!binding(),
    staleTime: Infinity,
  }));
  const [state, setState] = createSignal<T>();
  createEffect(() => {
    const b = binding();
    const r = history.data;
    if (!b || !r) {
      setState(undefined);
      return;
    }
    setState(() => o.init(r));
    const off = (o.hub ?? viewHub()).subscribe(b, r.cursor, {
      rows: (t) => setState((s) => (s === undefined ? s : o.fold(s, t, Date.now()))),
      reset: () => void qc.invalidateQueries({ queryKey: viewRunKey(b) }),
    });
    onCleanup(off);
  });
  return { state, error: () => history.error };
}

// foldMetricBuckets folds live samples into a bucketed metric series as the
// server aggregates them: each into the step-wide bucket it falls in (opening
// one past the history's grid), and a bucket the window has passed is
// dropped. last is the newest sample to land, which is the newest by time for
// a series that reports in order.
export function foldMetricBuckets(
  buckets: ChartBucket[],
  rows: { ts: string; value: number }[],
  stepMs: number,
  windowMs: number,
  now: number,
): ChartBucket[] {
  const byStart = new Map(buckets.map((b) => [Date.parse(b.ts), b]));
  for (const r of rows) {
    const start = Math.floor(Date.parse(r.ts) / stepMs) * stepMs;
    const b = byStart.get(start);
    const v = r.value;
    if (!b || b.count === 0 || b.avg === null || b.min === null || b.max === null) {
      byStart.set(start, { ts: b?.ts ?? new Date(start).toISOString(), count: 1, min: v, max: v, avg: v, last: v });
      continue;
    }
    byStart.set(start, { ts: b.ts, count: b.count + 1, min: Math.min(b.min, v), max: Math.max(b.max, v), avg: (b.avg * b.count + v) / (b.count + 1), last: v });
  }
  return [...byStart.entries()]
    .filter(([start]) => start + stepMs > now - windowMs)
    .sort(([a], [b]) => a - b)
    .map(([, b]) => b);
}
