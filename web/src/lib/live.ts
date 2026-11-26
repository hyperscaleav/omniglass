// The console's one live stream (#2731, ADR-2731). Each tab holds one hub,
// and the hub holds one /views:watch connection for everything the page
// follows: the view bindings' rows (createViewBinding, the row-fold path for
// charts) and the topics' changed notices (liveQuery, the path for every
// other read). A notice is an invalidation, not the data: liveQuery refetches
// its query through the typed route it already reads, so authorization,
// redaction and the response's shape stay on that route.
//
// The hub owns the connection: it reconnects with jittered exponential
// backoff, closes the stream in a tab hidden past a short grace and resumes
// it on focus, and only while no stream can open does it fall back to a slow
// refresh. This file and lib/clock.ts are the only places in web/src a timer
// may drive a read (polling-guard.test.ts).
import { createEffect, createMemo, on, onCleanup } from "solid-js";
import { useQuery, useQueryClient, type QueryKey } from "@tanstack/solid-query";
import { sendRaw } from "../api/client";
import type { ViewBindingBody, ViewChangedEvent, ViewMarkEvent, ViewResetEvent, ViewRowsEvent, ViewTailBindingBody } from "../api/api.gen";

type ViewBinding = ViewBindingBody;
type ViewTail = ViewTailBindingBody;

// VIEW_MAX_BINDINGS is the server's cap on one watch's bindings: a page past
// it is split across as few streams as fit. The topics ride the first.
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
// comment line (a keepalive) or a field-only frame (retry) is skipped.
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

export type TopicListener = {
  // changed says a write moved the topic's entity: refetch what shows it.
  changed: (topic: string) => void;
  // refresh says changes may have been missed (a reset, a stream newly
  // opened, the fallback refresh): refetch what was read before since, or
  // everything when since is unset.
  refresh: (since?: number) => void;
};

export type StreamOpener = (path: string, signal: AbortSignal) => Promise<Response>;

// Visibility is the part of the document the hub watches: whether the tab
// is hidden, and the event that says it changed.
export type Visibility = Pick<Document, "visibilityState" | "addEventListener" | "removeEventListener">;

type Entry = { key: string; binding: ViewBinding; joinable: string; cursor: string; listeners: Set<ViewListener> };

export type ViewHubOptions = {
  open: StreamOpener;
  // settleMs lets a burst of mounts and unmounts settle into one reconnect.
  settleMs?: number;
  // retryMs is the first reconnect delay after a failure, doubling (with
  // jitter) to maxRetryMs.
  retryMs?: number;
  maxRetryMs?: number;
  maxBindings?: number;
  // firstEventMs is how long an attempt waits for its first event before the
  // hub falls back to refreshing every fallbackMs (a buffering proxy, a
  // refused open, the per-user cap); the first event ends the fallback.
  firstEventMs?: number;
  fallbackMs?: number;
  // hiddenGraceMs is how long a hidden tab keeps its stream.
  hiddenGraceMs?: number;
  visibility?: Visibility;
  random?: () => number;
};

// ViewHub multiplexes a page's bindings and topics onto one /views:watch
// stream. A change to the set (a surface mounting or leaving) settles
// briefly, then reconnects every binding from the cursor the stream had
// reached for it, and the topics from the stream's last event id.
export class ViewHub {
  private entries = new Map<string, Entry>();
  private topics = new Map<string, Set<TopicListener>>();
  // topicsCursor is the last event id the topics' stream sent: every
  // change up to it has been delivered. Empty is a stream not yet opened.
  private topicsCursor = "";
  private streams: AbortController[] = [];
  private timer: ReturnType<typeof setTimeout> | undefined;
  private firstEventTimer: ReturnType<typeof setTimeout> | undefined;
  private fallbackTimer: ReturnType<typeof setInterval> | undefined;
  private hiddenTimer: ReturnType<typeof setTimeout> | undefined;
  private paused = false;
  private attempt = 0;
  private seq = 0;
  private readonly o: Required<Omit<ViewHubOptions, "visibility">> & { visibility?: Visibility };

  constructor(o: ViewHubOptions) {
    this.o = {
      settleMs: 50,
      retryMs: 1000,
      maxRetryMs: 30_000,
      maxBindings: VIEW_MAX_BINDINGS,
      firstEventMs: 10_000,
      fallbackMs: 30_000,
      hiddenGraceMs: 30_000,
      random: Math.random,
      ...o,
    };
    o.visibility?.addEventListener("visibilitychange", () => this.onVisibility());
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
      this.reconnect(this.o.settleMs);
    }
    e.listeners.add(l);
    const entry = e;
    return () => {
      entry.listeners.delete(l);
      if (entry.listeners.size === 0 && this.entries.delete(entry.key)) this.reconnect(this.o.settleMs);
    };
  }

  // watch follows topics (kind:ref), and returns the unwatch. The stream
  // reconnects only when the set of topics followed changes.
  watch(topics: string[], l: TopicListener): () => void {
    let added = false;
    for (const t of new Set(topics)) {
      let set = this.topics.get(t);
      if (!set) {
        set = new Set();
        this.topics.set(t, set);
        added = true;
      }
      set.add(l);
    }
    if (added) this.reconnect(this.o.settleMs);
    return () => {
      let removed = false;
      for (const t of new Set(topics)) {
        const set = this.topics.get(t);
        if (!set) continue;
        set.delete(l);
        if (set.size === 0) {
          this.topics.delete(t);
          removed = true;
        }
      }
      if (removed) this.reconnect(this.o.settleMs);
    };
  }

  // degraded reports whether the hub is refreshing on a timer because no
  // stream would open.
  degraded(): boolean {
    return this.fallbackTimer !== undefined;
  }

  private topicListeners(): Set<TopicListener> {
    return new Set([...this.topics.values()].flatMap((s) => [...s]));
  }

  private reconnect(delay: number) {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.connect(), delay);
  }

  // backoff is the next failed attempt's delay: exponential from retryMs,
  // capped, with jitter so every tab does not land together after a restart.
  private backoff(): number {
    const ceiling = Math.min(this.o.maxRetryMs, this.o.retryMs * 2 ** this.attempt);
    this.attempt++;
    return Math.round(ceiling * (0.5 + this.o.random() / 2));
  }

  private connect() {
    for (const c of this.streams) c.abort();
    this.streams = [];
    clearTimeout(this.firstEventTimer);
    if (this.paused) return;
    const all = [...this.entries.values()];
    const topics = [...this.topics.keys()];
    if (all.length === 0 && topics.length === 0) {
      this.stopFallback();
      return;
    }
    const chunks: Entry[][] = [];
    for (let i = 0; i < all.length; i += this.o.maxBindings) chunks.push(all.slice(i, i + this.o.maxBindings));
    if (chunks.length === 0) chunks.push([]);
    this.firstEventTimer = setTimeout(() => this.startFallback(), this.o.firstEventMs);
    chunks.forEach((list, i) => {
      const ctl = new AbortController();
      this.streams.push(ctl);
      void this.follow(list, i === 0 ? topics : [], ctl);
    });
  }

  private onFirstEvent() {
    clearTimeout(this.firstEventTimer);
    this.attempt = 0;
    this.stopFallback();
  }

  // startFallback refreshes every topic on a slow timer while no stream
  // delivers: the page stays roughly current, never silently frozen.
  private startFallback() {
    if (this.fallbackTimer !== undefined || this.paused || this.topics.size === 0) return;
    this.fallbackTimer = setInterval(() => {
      for (const l of this.topicListeners()) l.refresh();
    }, this.o.fallbackMs);
  }

  private stopFallback() {
    clearInterval(this.fallbackTimer);
    this.fallbackTimer = undefined;
  }

  // onVisibility closes the stream once the tab has been hidden past the
  // grace (a browser allows a few sockets per origin on HTTP/1.1, and a tab
  // no one sees needs none), and reopens it from where it was on focus: the
  // changes missed while hidden arrive as notices, or as a reset.
  private onVisibility() {
    const hidden = this.o.visibility?.visibilityState === "hidden";
    clearTimeout(this.hiddenTimer);
    if (hidden) {
      this.hiddenTimer = setTimeout(() => {
        this.paused = true;
        clearTimeout(this.timer);
        clearTimeout(this.firstEventTimer);
        this.stopFallback();
        for (const c of this.streams) c.abort();
        this.streams = [];
      }, this.o.hiddenGraceMs);
      return;
    }
    if (this.paused) {
      this.paused = false;
      this.attempt = 0;
      this.connect();
    }
  }

  private path(list: Entry[], topics: string[]): string {
    const q: string[] = [];
    if (list.length > 0) {
      const bindings = list.map((e) => ({ ...e.binding, cursor: e.cursor }));
      q.push(`bindings=${encodeURIComponent(JSON.stringify(bindings))}`);
    }
    if (topics.length > 0) {
      q.push(`topics=${encodeURIComponent(topics.join(","))}`);
      if (this.topicsCursor) q.push(`topics_cursor=${encodeURIComponent(this.topicsCursor)}`);
    }
    return `/views:watch?${q.join("&")}`;
  }

  private async follow(list: Entry[], topics: string[], ctl: AbortController) {
    const opened = Date.now();
    // A stream opened with no position may have missed a change made
    // between a query's read and the server resolving its start: once it
    // opens, what was read before it refetches.
    let fresh = topics.length > 0 && !this.topicsCursor;
    let reset = false;
    let first = true;
    const parse = sseParser((ev) => {
      if (ctl.signal.aborted) return;
      if (first) {
        first = false;
        this.onFirstEvent();
      }
      if (topics.length > 0 && ev.id) this.topicsCursor = ev.id;
      if (ev.event === "rows") {
        const body = JSON.parse(ev.data) as ViewRowsEvent;
        for (const t of body.bindings ?? []) for (const l of list[t.binding]?.listeners ?? []) l.rows(t);
        for (const e of list) e.cursor = laterCursor(e.cursor, body.cursor);
      } else if (ev.event === "mark") {
        const body = JSON.parse(ev.data) as ViewMarkEvent;
        for (const e of list) e.cursor = laterCursor(e.cursor, body.cursor);
        if (fresh) {
          fresh = false;
          for (const l of this.topicListeners()) l.refresh(opened);
        }
      } else if (ev.event === "changed") {
        const body = JSON.parse(ev.data) as ViewChangedEvent;
        for (const l of this.topics.get(body.topic) ?? []) l.changed(body.topic);
      } else if (ev.event === "reset") {
        reset = true;
        const body = JSON.parse(ev.data) as ViewResetEvent;
        if (body.topics) {
          this.topicsCursor = "";
          for (const l of this.topicListeners()) l.refresh();
        }
        for (const i of body.bindings ?? []) {
          const e = list[i];
          if (!e || !this.entries.delete(e.key)) continue;
          for (const l of e.listeners) l.reset();
        }
      }
    });
    let ended = false;
    try {
      const res = await this.o.open(this.path(list, topics), ctl.signal);
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
    // ends every watch at its lifetime, when its caller's access changed, or
    // as it shuts down): the reconnect carries on from its cursor. A stream
    // refused, dropped, or ended as soon as it opened backs off, and a
    // refused one falls back to the slow refresh until a stream opens.
    if (!ended) this.startFallback();
    const ranItsCourse = ended && !first && Date.now() - opened >= this.o.retryMs;
    this.reconnect(reset || ranItsCourse ? this.o.settleMs : this.backoff());
  }
}

let shared: ViewHub | undefined;

// viewHub is the console's one hub: every binding and topic on the page
// shares it.
export function viewHub(): ViewHub {
  shared ??= new ViewHub({
    open: (url, signal) => sendRaw(url, { signal, headers: { Accept: "text/event-stream" } }),
    visibility: typeof document === "undefined" ? undefined : document,
  });
  return shared;
}

// LIVE_DEBOUNCE_MS gathers a burst of notices into one refetch.
export const LIVE_DEBOUNCE_MS = 250;

// liveQuery is useQuery that follows topics: it registers them with the
// tab's hub and refetches its query when one changes (debounced), and when
// the hub says changes may have been missed. It never polls. A query that is
// not enabled is only marked stale, and refetches once it is.
export function liveQuery<TData>(
  o: () => {
    queryKey: QueryKey;
    queryFn: () => Promise<TData>;
    topics: string[];
    enabled?: boolean;
  },
  hub?: ViewHub,
) {
  const qc = useQueryClient();
  const q = useQuery(() => {
    const { topics: _topics, ...rest } = o();
    return rest;
  });
  // The registration keys on what it follows, so a reactive read elsewhere
  // in o (enabled, say) does not churn the stream.
  const follows = createMemo(() => JSON.stringify([o().queryKey, [...o().topics].sort()]));
  createEffect(
    on(follows, (k) => {
      const [queryKey, topics] = JSON.parse(k) as [QueryKey, string[]];
      if (topics.length === 0) return;
      let timer: ReturnType<typeof setTimeout> | undefined;
      const refetch = () => void qc.invalidateQueries({ queryKey, exact: true });
      const off = (hub ?? viewHub()).watch(topics, {
        changed: () => {
          clearTimeout(timer);
          timer = setTimeout(refetch, LIVE_DEBOUNCE_MS);
        },
        refresh: (since) => {
          const at = qc.getQueryState(queryKey)?.dataUpdatedAt ?? 0;
          if (since === undefined || at < since) refetch();
        },
      });
      onCleanup(() => {
        clearTimeout(timer);
        off();
      });
    }),
  );
  return q;
}
