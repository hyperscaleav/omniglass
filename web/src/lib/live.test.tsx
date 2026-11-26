import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { JSX } from "solid-js";
import { LIVE_DEBOUNCE_MS, ViewHub, liveQuery, type TopicListener, type Visibility } from "./live";

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
    this.response = new Response(status === 200 ? body : null, { status, headers: { "content-type": "text/event-stream" } });
  }
  query(name: string): string | null {
    return new URLSearchParams(this.path.split("?")[1]).get(name);
  }
  send(event: string, data: unknown, id = "") {
    this.ctl.enqueue(new TextEncoder().encode(`${id ? `id: ${id}\n` : ""}event: ${event}\ndata: ${JSON.stringify(data)}\n\n`));
  }
  end() {
    this.ctl.close();
  }
}

// A tab whose visibility the test flips.
class FakeVisibility extends EventTarget {
  visibilityState: DocumentVisibilityState = "visible";
  set(state: DocumentVisibilityState) {
    this.visibilityState = state;
    this.dispatchEvent(new Event("visibilitychange"));
  }
}

const listener = () => {
  const l = { changed: vi.fn<(t: string) => void>(), refresh: vi.fn<(since?: number) => void>() };
  return l satisfies TopicListener;
};

describe("ViewHub topics", () => {
  let opened: FakeStream[];
  let status: number;
  let vis: FakeVisibility;
  let random: number;
  const make = (extra: Partial<ConstructorParameters<typeof ViewHub>[0]> = {}) =>
    new ViewHub({
      open: (path, signal) => {
        const s = new FakeStream(path, signal, status);
        opened.push(s);
        return Promise.resolve(s.response);
      },
      settleMs: 10,
      retryMs: 1000,
      maxRetryMs: 8000,
      firstEventMs: 10_000,
      fallbackMs: 30_000,
      hiddenGraceMs: 5000,
      visibility: vis as unknown as Visibility,
      random: () => random,
      ...extra,
    });
  beforeEach(() => {
    vi.useFakeTimers();
    opened = [];
    status = 200;
    random = 1;
    vis = new FakeVisibility();
  });
  afterEach(() => vi.useRealTimers());
  const live = () => opened.filter((s) => !s.signal.aborted);
  const settle = (ms = 10) => vi.advanceTimersByTimeAsync(ms);

  it("follows every topic on the one stream beside the bindings, and hands each changed notice to its topic's listeners", async () => {
    const hub = make();
    const a = listener();
    const b = listener();
    hub.watch(["system:s1"], a);
    hub.watch(["system:s1", "component:c1"], b);
    hub.subscribe({ primitive: "metric", kind: "component", refs: ["c1"], keys: ["k"], window_seconds: 60 }, "boot.1", { rows: () => {}, reset: () => {} });
    await settle();
    expect(live()).toHaveLength(1);
    expect(live()[0].query("topics")).toBe("system:s1,component:c1");
    expect(live()[0].query("bindings")).not.toBeNull();
    live()[0].send("changed", { topic: "component:c1", kind: "component", id: "c1", op: "update", v: 3 }, "boot.3");
    await settle(0);
    expect(a.changed).not.toHaveBeenCalled();
    expect(b.changed).toHaveBeenCalledWith("component:c1");
  });

  it("reconnects only when the set of topics changes, resuming the topics from the last event id", async () => {
    const hub = make();
    const offA = hub.watch(["system:s1"], listener());
    await settle();
    expect(live()[0].query("topics_cursor")).toBeNull();
    live()[0].send("mark", { cursor: "boot.4" }, "boot.4");
    await settle(0);
    hub.watch(["system:s1"], listener());
    await settle();
    expect(opened).toHaveLength(1);
    hub.watch(["system:s2"], listener());
    await settle();
    expect(live()).toHaveLength(1);
    expect(opened).toHaveLength(2);
    expect(live()[0].query("topics")).toBe("system:s1,system:s2");
    expect(live()[0].query("topics_cursor")).toBe("boot.4");
    offA();
    await settle();
    expect(opened).toHaveLength(2);
  });

  it("refreshes what was read before a fresh stream opened, once it opens", async () => {
    const hub = make();
    const l = listener();
    hub.watch(["system:s1"], l);
    await settle();
    live()[0].send("mark", { cursor: "boot.1" }, "boot.1");
    live()[0].send("mark", { cursor: "boot.1" }, "boot.1");
    await settle(0);
    expect(l.refresh).toHaveBeenCalledTimes(1);
    expect(typeof l.refresh.mock.calls[0][0]).toBe("number");
  });

  it("on a reset naming the topics, refreshes everything they show and reconnects with no position", async () => {
    const hub = make();
    const l = listener();
    hub.watch(["system:s1"], l);
    await settle();
    live()[0].send("mark", { cursor: "boot.4" }, "boot.4");
    await settle(0);
    hub.watch(["system:s2"], listener());
    await settle();
    l.refresh.mockClear();
    live()[0].send("reset", { bindings: [], topics: true });
    live()[0].end();
    await settle();
    expect(l.refresh).toHaveBeenCalledWith();
    expect(live()[0].query("topics_cursor")).toBeNull();
  });

  it("backs off a refused stream exponentially with jitter, capped, and starts over once one opens", async () => {
    status = 503;
    random = 0;
    const hub = make();
    hub.watch(["system:s1"], listener());
    await settle();
    expect(opened).toHaveLength(1);
    // Jitter at its floor: half of 1s, 2s, 4s, 8s, then the 8s cap.
    for (const wait of [500, 1000, 2000, 4000, 4000]) {
      await settle(wait - 1);
      const before = opened.length;
      await settle(1);
      expect(opened.length).toBe(before + 1);
    }
    status = 200;
    await settle(4000);
    live()[0].send("mark", { cursor: "boot.1" }, "boot.1");
    live()[0].end();
    await settle(0);
    // Ended at once after opening, it backs off again, from the start.
    const before = opened.length;
    await settle(499);
    expect(opened.length).toBe(before);
    await settle(1);
    expect(opened.length).toBe(before + 1);
  });

  it("spreads its reconnects by the jitter, so tabs do not land together", async () => {
    status = 503;
    const at = async (r: number) => {
      opened = [];
      random = r;
      const hub = make();
      hub.watch(["system:s1"], listener());
      await settle();
      let waited = 0;
      while (opened.length < 2) {
        await settle(50);
        waited += 50;
      }
      return waited;
    };
    expect(await at(0)).toBe(500);
    expect(await at(1)).toBe(1000);
  });

  it("closes the stream in a tab hidden past the grace, and on focus resumes from where it was", async () => {
    const hub = make();
    hub.watch(["system:s1"], listener());
    await settle();
    live()[0].send("mark", { cursor: "boot.7" }, "boot.7");
    await settle(0);
    vis.set("hidden");
    await settle(4999);
    expect(live()).toHaveLength(1);
    await settle(1);
    expect(live()).toHaveLength(0);
    // A topic added while hidden opens nothing.
    hub.watch(["system:s2"], listener());
    await settle(60_000);
    expect(live()).toHaveLength(0);
    vis.set("visible");
    await settle(0);
    expect(live()).toHaveLength(1);
    expect(live()[0].query("topics_cursor")).toBe("boot.7");
    expect(live()[0].query("topics")).toBe("system:s1,system:s2");
  });

  it("keeps the stream through a short hide", async () => {
    const hub = make();
    hub.watch(["system:s1"], listener());
    await settle();
    vis.set("hidden");
    await settle(1000);
    vis.set("visible");
    await settle(10_000);
    expect(opened).toHaveLength(1);
    expect(live()).toHaveLength(1);
  });

  it("falls back to a slow refresh while no stream opens, and stops once one delivers", async () => {
    status = 429;
    const hub = make({ maxRetryMs: 60_000, retryMs: 60_000 });
    const l = listener();
    hub.watch(["system:s1"], l);
    await settle();
    expect(hub.degraded()).toBe(true);
    await settle(30_000);
    expect(l.refresh).toHaveBeenCalledTimes(1);
    status = 200;
    await settle(30_000);
    live()[0].send("mark", { cursor: "boot.1" }, "boot.1");
    await settle(0);
    expect(hub.degraded()).toBe(false);
    const n = l.refresh.mock.calls.length;
    await settle(90_000);
    expect(l.refresh.mock.calls.length).toBe(n);
  });

  it("falls back when a stream opens but no event arrives (a buffering proxy)", async () => {
    const hub = make();
    const l = listener();
    hub.watch(["system:s1"], l);
    await settle();
    await settle(9_990);
    expect(hub.degraded()).toBe(false);
    await settle(10);
    expect(hub.degraded()).toBe(true);
    await settle(30_000);
    expect(l.refresh).toHaveBeenCalled();
    live()[0].send("mark", { cursor: "boot.1" }, "boot.1");
    await settle(0);
    expect(hub.degraded()).toBe(false);
  });
});

describe("liveQuery", () => {
  afterEach(() => vi.useRealTimers());

  it("reads once, never polls, and refetches once for a burst of changed notices", async () => {
    vi.useFakeTimers();
    const opened: FakeStream[] = [];
    const hub = new ViewHub({
      open: (path, signal) => {
        const s = new FakeStream(path, signal);
        opened.push(s);
        return Promise.resolve(s.response);
      },
      settleMs: 0,
    });
    let reads = 0;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = (p: { children: JSX.Element }) => <QueryClientProvider client={qc}>{p.children}</QueryClientProvider>;
    const { result } = renderHook(
      () => liveQuery(() => ({ queryKey: ["health", "s1"], queryFn: async () => ++reads, topics: ["system:s1"] }), hub),
      { wrapper },
    );
    await vi.advanceTimersByTimeAsync(10);
    expect(result.data).toBe(1);
    expect(opened).toHaveLength(1);
    opened[0].send("mark", { cursor: "boot.1" }, "boot.1");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(reads).toBe(1);
    for (let v = 2; v < 7; v++) opened[0].send("changed", { topic: "system:s1", kind: "system", id: "s1", op: "update", v }, `boot.${v}`);
    await vi.advanceTimersByTimeAsync(LIVE_DEBOUNCE_MS - 1);
    expect(reads).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    await vi.advanceTimersByTimeAsync(0);
    expect(reads).toBe(2);
    expect(result.data).toBe(2);
    opened[0].send("changed", { topic: "system:s2", kind: "system", id: "s2", op: "update", v: 9 }, "boot.9");
    await vi.advanceTimersByTimeAsync(1000);
    expect(reads).toBe(2);
  });
});
