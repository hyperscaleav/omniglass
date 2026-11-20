import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { JSX } from "solid-js";
import { ViewHub, bindingKey, createViewBinding, foldMetricBuckets, laterCursor, sseParser, type ViewBinding, type ViewTail } from "./binding";
import type { ChartBucket } from "./chart";

const metric = (keys: string[]): ViewBinding => ({ primitive: "metric", kind: "component", refs: ["server"], keys, window_seconds: 3600, step_seconds: 60 });

describe("bindingKey", () => {
  it("names a binding by its content, whatever order its fields were written in", () => {
    expect(bindingKey({ kind: "component", primitive: "metric", keys: ["a"] })).toBe(bindingKey({ primitive: "metric", keys: ["a"], kind: "component" }));
    expect(bindingKey(metric(["a"]))).not.toBe(bindingKey(metric(["b"])));
  });
});

describe("sseParser", () => {
  it("frames events across chunk boundaries, joins data lines and skips comments", () => {
    const got: { id: string; event: string; data: string }[] = [];
    const push = sseParser((e) => got.push(e));
    push("id: a.1\nevent: ro");
    push("ws\ndata: {\"x\":\ndata: 1}\n\n: keepalive\n\n");
    push("event: mark\r\ndata: {}\r\n\r\n");
    expect(got).toEqual([
      { id: "a.1", event: "rows", data: '{"x":\n1}' },
      { id: "", event: "mark", data: "{}" },
    ]);
  });
});

describe("foldMetricBuckets", () => {
  const t0 = Date.parse("2026-01-01T10:00:00Z");
  const at = (ms: number) => new Date(t0 + ms).toISOString();
  const bucket = (ms: number, values: number[]): ChartBucket =>
    values.length === 0
      ? { ts: at(ms), count: 0, min: null, max: null, avg: null, last: null }
      : { ts: at(ms), count: values.length, min: Math.min(...values), max: Math.max(...values), avg: values.reduce((a, b) => a + b, 0) / values.length, last: values[values.length - 1] };

  it("folds a live sample into the bucket it falls in, as the server would have aggregated it", () => {
    const out = foldMetricBuckets([bucket(0, [2, 4]), bucket(60_000, [])], [{ ts: at(10_000), value: 9 }, { ts: at(70_000), value: 5 }], 60_000, 3600_000, t0 + 80_000);
    expect(out).toEqual([bucket(0, [2, 4, 9]), bucket(60_000, [5])]);
  });

  it("opens a bucket past the history's grid, and drops a bucket the window has passed", () => {
    // The window opens at 130s, so the grid (as the server aligns it) starts at the 120s bucket.
    const out = foldMetricBuckets([bucket(0, [1]), bucket(60_000, [2])], [{ ts: at(180_000), value: 3 }], 60_000, 120_000, t0 + 250_000);
    expect(out.map((b) => b.ts)).toEqual([at(180_000)]);
    expect(out[0]).toEqual(bucket(180_000, [3]));
  });

  it("leaves the buckets it was given untouched", () => {
    const given = [bucket(0, [1])];
    foldMetricBuckets(given, [{ ts: at(1000), value: 7 }], 60_000, 3600_000, t0 + 2000);
    expect(given).toEqual([bucket(0, [1])]);
  });
});

// A fake stream the hub opens: the test writes the server's events into it.
class FakeStream {
  path: string;
  signal: AbortSignal;
  private ctl!: ReadableStreamDefaultController<Uint8Array>;
  readonly response: Response;
  constructor(path: string, signal: AbortSignal, status = 200) {
    this.path = path;
    this.signal = signal;
    const body = new ReadableStream<Uint8Array>({ start: (c) => (this.ctl = c) });
    signal.addEventListener("abort", () => {
      try {
        this.ctl.close();
      } catch {
        // already closed
      }
    });
    this.response = new Response(body, { status, headers: { "content-type": "text/event-stream" } });
  }
  bindings(): (ViewBinding & { cursor: string })[] {
    return JSON.parse(new URLSearchParams(this.path.split("?")[1]).get("bindings")!);
  }
  send(event: string, data: unknown, id = "") {
    this.ctl.enqueue(new TextEncoder().encode(`${id ? `id: ${id}\n` : ""}event: ${event}\ndata: ${JSON.stringify(data)}\n\n`));
  }
  end() {
    this.ctl.close();
  }
}

describe("ViewHub", () => {
  let opened: FakeStream[];
  let hub: ViewHub;
  beforeEach(() => {
    vi.useFakeTimers();
    opened = [];
    hub = new ViewHub({
      open: (path, signal) => {
        const s = new FakeStream(path, signal);
        opened.push(s);
        return Promise.resolve(s.response);
      },
      settleMs: 10,
      retryMs: 1000,
    });
  });
  afterEach(() => vi.useRealTimers());

  const live = () => opened.filter((s) => !s.signal.aborted);
  const settle = async (ms = 10) => {
    await vi.advanceTimersByTimeAsync(ms);
  };

  it("serves every subscribed binding over one stream, each from its own cursor, and hands each its own rows", async () => {
    const a: ViewTail[] = [];
    const b: ViewTail[] = [];
    hub.subscribe(metric(["a"]), "boot.4", { rows: (t) => a.push(t), reset: () => {} });
    hub.subscribe(metric(["b"]), "boot.7", { rows: (t) => b.push(t), reset: () => {} });
    await settle();
    expect(live()).toHaveLength(1);
    expect(live()[0].path.startsWith("/views:watch?bindings=")).toBe(true);
    expect(live()[0].bindings().map((x) => [x.keys, x.cursor])).toEqual([[["a"], "boot.4"], [["b"], "boot.7"]]);
    live()[0].send("rows", { cursor: "boot.8", bindings: [{ binding: 1, metric: [{ entity: "e", key: "b", instance: "", ts: "t", value: 1 }] }] }, "boot.8");
    await settle(0);
    expect(a).toEqual([]);
    expect(b).toHaveLength(1);
    expect(b[0].metric?.[0].value).toBe(1);
  });

  it("shares one binding between subscribers of the same history, and reconnects from where the stream reached when the set changes", async () => {
    const seen: number[] = [];
    const off = hub.subscribe(metric(["a"]), "boot.4", { rows: () => seen.push(1), reset: () => {} });
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => seen.push(2), reset: () => {} });
    await settle();
    expect(live()[0].bindings()).toHaveLength(1);
    live()[0].send("mark", { cursor: "boot.9" }, "boot.9");
    await settle(0);
    hub.subscribe(metric(["b"]), "boot.9", { rows: () => {}, reset: () => {} });
    await settle();
    expect(live()).toHaveLength(1);
    expect(live()[0].bindings().map((x) => x.cursor)).toEqual(["boot.9", "boot.9"]);
    off();
    live()[0].send("rows", { cursor: "boot.10", bindings: [{ binding: 0, metric: [] }] });
    await settle(0);
    expect(seen).toEqual([2]);
  });

  it("does not join a subscriber to a binding the stream has carried past its history's cursor", async () => {
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => {} });
    await settle();
    live()[0].send("mark", { cursor: "boot.9" });
    await settle(0);
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => {} });
    await settle();
    expect(live()[0].bindings().map((x) => x.cursor)).toEqual(["boot.9", "boot.4"]);
  });

  it("never moves a binding's cursor back to a stream position below it, so a reconnect resends nothing its history holds", async () => {
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => {} });
    hub.subscribe(metric(["b"]), "boot.9", { rows: () => {}, reset: () => {} });
    await settle();
    // The stream starts at the slowest binding: its first positions are
    // below b's history.
    live()[0].send("mark", { cursor: "boot.5" }, "boot.5");
    live()[0].send("rows", { cursor: "boot.6", bindings: [{ binding: 0, metric: [] }] }, "boot.6");
    await settle(0);
    hub.subscribe(metric(["c"]), "boot.6", { rows: () => {}, reset: () => {} });
    await settle();
    expect(live()).toHaveLength(1);
    expect(live()[0].bindings().map((x) => [x.keys, x.cursor])).toEqual([[["a"], "boot.6"], [["b"], "boot.9"], [["c"], "boot.6"]]);
    live()[0].send("mark", { cursor: "boot.12" }, "boot.12");
    await settle(0);
    live()[0].end();
    await settle(1000);
    expect(live()[0].bindings().map((x) => x.cursor)).toEqual(["boot.12", "boot.12", "boot.12"]);
  });

  it("orders cursors by their sequence, not their text", () => {
    expect(laterCursor("boot.9", "boot.10")).toBe("boot.10");
    expect(laterCursor("boot.10", "boot.9")).toBe("boot.10");
    expect(laterCursor("old.40", "new.2")).toBe("new.2");
  });

  it("on a reset, tells the reset binding to re-read its history and reconnects the others at once", async () => {
    let resets = 0;
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => resets++ });
    hub.subscribe(metric(["b"]), "boot.4", { rows: () => {}, reset: () => {} });
    await settle();
    live()[0].send("reset", { bindings: [0] });
    live()[0].end();
    await settle();
    expect(resets).toBe(1);
    expect(live()).toHaveLength(1);
    expect(live()[0].bindings().map((x) => x.keys)).toEqual([["b"]]);
  });

  it("retries a dropped stream after a pause, from the last cursor it delivered", async () => {
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => {} });
    await settle();
    live()[0].send("rows", { cursor: "boot.6", bindings: [] }, "boot.6");
    live()[0].end();
    await settle();
    expect(opened).toHaveLength(1);
    await settle(1000);
    expect(opened).toHaveLength(2);
    expect(opened[1].bindings()[0].cursor).toBe("boot.6");
  });

  it("reconnects at once from its cursor when a stream that ran ends, as the server ends one at its lifetime", async () => {
    hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => {} });
    await settle();
    await settle(1000);
    live()[0].send("mark", { cursor: "boot.9" }, "boot.9");
    live()[0].end();
    await settle();
    expect(opened).toHaveLength(2);
    expect(opened[1].bindings()[0].cursor).toBe("boot.9");
  });

  it("closes the stream when the last subscriber leaves", async () => {
    const off = hub.subscribe(metric(["a"]), "boot.4", { rows: () => {}, reset: () => {} });
    await settle();
    off();
    await settle();
    expect(live()).toHaveLength(0);
    expect(opened).toHaveLength(1);
  });

  it("splits past the server's binding cap into as few streams as fit", async () => {
    const small = new ViewHub({
      open: (path, signal) => {
        const s = new FakeStream(path, signal);
        opened.push(s);
        return Promise.resolve(s.response);
      },
      settleMs: 10,
      maxBindings: 2,
    });
    for (const k of ["a", "b", "c"]) small.subscribe(metric([k]), "boot.1", { rows: () => {}, reset: () => {} });
    await settle();
    expect(live().map((s) => s.bindings().length)).toEqual([2, 1]);
  });
});

describe("createViewBinding", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("starts from the binding's history, folds its live rows in, and on a reset starts again from a fresh history", async () => {
    const cursors = ["boot.1", "boot.5"];
    const runs: unknown[] = [];
    vi.stubGlobal("fetch", async (req: Request) => {
      runs.push(await req.json());
      const cursor = cursors[runs.length - 1];
      const result = { cursor, covered_from: "", from: "", to: "", entities: [], metric: [] };
      return new Response(JSON.stringify({ cursor, now: "", results: [result] }), { status: 200, headers: { "content-type": "application/json" } });
    });
    const opened: FakeStream[] = [];
    const hub = new ViewHub({
      open: (path, signal) => {
        const s = new FakeStream(path, signal);
        opened.push(s);
        return Promise.resolve(s.response);
      },
      settleMs: 0,
    });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = (p: { children: JSX.Element }) => <QueryClientProvider client={qc}>{p.children}</QueryClientProvider>;
    const { result } = renderHook(
      () => createViewBinding({ binding: () => metric(["a"]), init: (r) => r.cursor, fold: (s, t) => `${s}+${t.metric?.length}`, hub }),
      { wrapper },
    );

    await vi.waitFor(() => expect(opened).toHaveLength(1));
    expect(result.state()).toBe("boot.1");
    expect(runs).toEqual([{ bindings: [metric(["a"])] }]);
    expect(opened[0].bindings()[0].cursor).toBe("boot.1");

    opened[0].send("rows", { cursor: "boot.2", bindings: [{ binding: 0, metric: [{ entity: "e", key: "a", instance: "", ts: "t", value: 1 }] }] });
    await vi.waitFor(() => expect(result.state()).toBe("boot.1+1"));

    opened[0].send("reset", { bindings: [0] });
    opened[0].end();
    await vi.waitFor(() => expect(result.state()).toBe("boot.5"));
    await vi.waitFor(() => expect(opened.filter((s) => !s.signal.aborted).map((s) => s.bindings()[0].cursor)).toEqual(["boot.5"]));
    expect(runs).toHaveLength(2);
  });
});
