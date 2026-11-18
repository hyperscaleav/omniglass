// The views binding (#1830, ADR-1830): one scoped read a surface binds to,
// its history from /views:run and its live rows from /views:watch. A page
// binds as many as it draws, and every binding on it rides one stream: the
// hub collects them, opens /views:watch once with each binding's cursor, and
// hands each its own rows. History and live meet at the cursor the run
// answered, so a fold sees every row exactly once.
import { createEffect, createMemo, createSignal, onCleanup } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api, sendRaw } from "../api/client";
import type { ViewBindingBody, ViewMarkEvent, ViewResetEvent, ViewResultBody, ViewRowsEvent, ViewTailBindingBody } from "../api/api.gen";
import type { ChartBucket } from "./chart";

export type ViewBinding = ViewBindingBody;
export type ViewResult = ViewResultBody;
// ViewTail is one binding's share of a rows event: the rows landed for it.
export type ViewTail = ViewTailBindingBody;

// VIEW_MAX_BINDINGS is the server's cap on one watch's bindings: a page past
// it is split across as few streams as fit.
export const VIEW_MAX_BINDINGS = 32;

// bindingKey names a binding by its content, so two surfaces binding the same
// read share its history and its place on the stream.
export function bindingKey(b: ViewBinding): string {
  const sorted = (v: unknown): unknown =>
    Array.isArray(v)
      ? v.map(sorted)
      : v && typeof v === "object"
        ? Object.fromEntries(Object.keys(v).sort().map((k) => [k, sorted((v as Record<string, unknown>)[k])]))
        : v;
  return JSON.stringify(sorted(b));
}

export const viewRunKey = (b: ViewBinding | undefined) => ["view-run", b ? bindingKey(b) : null] as const;

export async function runView(b: ViewBinding): Promise<ViewResult> {
  const res = await api.POST("/views:run", { body: { bindings: [b] } });
  if (res.error) throw res.error;
  const result = res.data?.results?.[0];
  if (!result) throw new Error("views run answered no result");
  return result;
}

// laterCursor is the later of a binding's cursor and a stream position. A
// cursor is "<boot>.<sequence>", ordered by its sequence on one boot; the
// stream starts at its slowest binding, so its position can sit below a
// binding's own cursor, and moving that binding back would resend rows its
// history already holds. A position from another boot wins: the server resets
// a cursor it cannot order.
export function laterCursor(cursor: string, position: string): string {
  const seq = (c: string) => {
    const dot = c.lastIndexOf(".");
    return { boot: c.slice(0, dot), n: /^\d+$/.test(c.slice(dot + 1)) ? BigInt(c.slice(dot + 1)) : undefined };
  };
  const a = seq(cursor);
  const b = seq(position);
  if (a.boot !== b.boot || a.n === undefined || b.n === undefined) return position;
  return a.n > b.n ? cursor : position;
}

export type SseEvent = { id: string; event: string; data: string };

// sseParser frames a Server-Sent Events body pushed to it in chunks of any
// size: a blank line ends an event, data lines join with a newline, and a
// comment line (a keepalive) is skipped.
export function sseParser(onEvent: (e: SseEvent) => void): (chunk: string) => void {
  let buf = "";
  let ev: SseEvent = { id: "", event: "", data: "" };
  let hasData = false;
  return (chunk) => {
    buf += chunk;
    let nl: number;
    while ((nl = buf.search(/\r?\n/)) >= 0) {
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + (buf[nl] === "\r" ? 2 : 1));
      if (line === "") {
        if (ev.event || hasData) onEvent(ev);
        ev = { id: "", event: "", data: "" };
        hasData = false;
        continue;
      }
      if (line.startsWith(":")) continue;
      const colon = line.indexOf(":");
      const field = colon < 0 ? line : line.slice(0, colon);
      const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
      if (field === "id") ev.id = value;
      else if (field === "event") ev.event = value;
      else if (field === "data") {
        ev.data = hasData ? `${ev.data}\n${value}` : value;
        hasData = true;
      }
    }
  };
}

export type ViewListener = {
  // rows hands the binding the rows landed for it, in commit order.
  rows: (t: ViewTail) => void;
  // reset says the stream can no longer resume the binding: re-read its
  // history, and bind again from the new cursor.
  reset: () => void;
};

export type StreamOpener = (path: string, signal: AbortSignal) => Promise<Response>;

type Entry = { key: string; binding: ViewBinding; joinable: string; cursor: string; listeners: Set<ViewListener> };

// ViewHub multiplexes a page's bindings onto one /views:watch stream. A change
// to the set (a surface mounting or leaving) settles briefly, then reconnects
// every binding from the cursor the stream had reached for it.
export class ViewHub {
  private entries = new Map<string, Entry>();
  private streams: AbortController[] = [];
  private timer: ReturnType<typeof setTimeout> | undefined;
  private seq = 0;
  private readonly open: StreamOpener;
  private readonly settleMs: number;
  private readonly retryMs: number;
  private readonly maxBindings: number;

  constructor(o: { open: StreamOpener; settleMs?: number; retryMs?: number; maxBindings?: number }) {
    this.open = o.open;
    this.settleMs = o.settleMs ?? 50;
    this.retryMs = o.retryMs ?? 5000;
    this.maxBindings = o.maxBindings ?? VIEW_MAX_BINDINGS;
  }

  // subscribe follows binding from cursor (its history's), and returns the
  // unsubscribe. Subscribers of the same history share its place on the
  // stream; one whose history is older than where the stream has carried the
  // binding gets a place of its own, so it misses nothing in between.
  subscribe(binding: ViewBinding, cursor: string, l: ViewListener): () => void {
    const base = `${bindingKey(binding)}@${cursor}`;
    let e = [...this.entries.values()].find((x) => x.joinable === base && x.cursor === cursor);
    if (!e) {
      e = { key: `${base}#${++this.seq}`, binding, joinable: base, cursor, listeners: new Set() };
      this.entries.set(e.key, e);
      this.reconnect(this.settleMs);
    }
    e.listeners.add(l);
    const entry = e;
    return () => {
      entry.listeners.delete(l);
      if (entry.listeners.size === 0 && this.entries.delete(entry.key)) this.reconnect(this.settleMs);
    };
  }

  private reconnect(delay: number) {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.connect(), delay);
  }

  private connect() {
    for (const c of this.streams) c.abort();
    this.streams = [];
    const all = [...this.entries.values()];
    for (let i = 0; i < all.length; i += this.maxBindings) {
      const ctl = new AbortController();
      this.streams.push(ctl);
      void this.follow(all.slice(i, i + this.maxBindings), ctl);
    }
  }

  private async follow(list: Entry[], ctl: AbortController) {
    const bindings = list.map((e) => ({ ...e.binding, cursor: e.cursor }));
    let reset = false;
    const parse = sseParser((ev) => {
      if (ctl.signal.aborted) return;
      if (ev.event === "rows") {
        const body = JSON.parse(ev.data) as ViewRowsEvent;
        for (const t of body.bindings ?? []) for (const l of list[t.binding]?.listeners ?? []) l.rows(t);
        for (const e of list) e.cursor = laterCursor(e.cursor, body.cursor);
      } else if (ev.event === "mark") {
        const body = JSON.parse(ev.data) as ViewMarkEvent;
        for (const e of list) e.cursor = laterCursor(e.cursor, body.cursor);
      } else if (ev.event === "reset") {
        reset = true;
        for (const i of (JSON.parse(ev.data) as ViewResetEvent).bindings ?? []) {
          const e = list[i];
          if (!e || !this.entries.delete(e.key)) continue;
          for (const l of e.listeners) l.reset();
        }
      }
    });
    const opened = Date.now();
    let ended = false;
    try {
      const res = await this.open(`/views:watch?bindings=${encodeURIComponent(JSON.stringify(bindings))}`, ctl.signal);
      if (!res.ok || !res.body) throw new Error(`views watch answered ${res.status}`);
      const reader = res.body.getReader();
      const text = new TextDecoder();
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        parse(text.decode(value, { stream: true }));
      }
      ended = true;
    } catch {
      // A refused or dropped stream retries below.
    }
    if (ctl.signal.aborted) return;
    // After a reset the server ends the stream, and the other bindings
    // resume at once. So does a stream that ran and then ended (the server
    // ends every watch at its lifetime, or when its caller's access changed):
    // the reconnect carries on from its cursor. A stream refused, dropped, or
    // ended as soon as it opened retries after a pause.
    const ranItsCourse = ended && Date.now() - opened >= this.retryMs;
    this.reconnect(reset || ranItsCourse ? this.settleMs : this.retryMs);
  }
}

let shared: ViewHub | undefined;

// viewHub is the console's one hub: every binding on the page shares it.
export function viewHub(): ViewHub {
  shared ??= new ViewHub({ open: (url, signal) => sendRaw(url, { signal, headers: { Accept: "text/event-stream" } }) });
  return shared;
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
