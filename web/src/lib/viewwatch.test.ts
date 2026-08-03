import { describe, expect, it, vi, afterEach } from "vitest";
import { setUnauthorizedHandler } from "../api/client";
import { createSSEParser, watchView } from "./viewwatch";

// A ReadableStream that the test feeds by hand, standing in for a live
// :watch response body.
function feedableStream() {
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  const enc = new TextEncoder();
  return {
    stream,
    feed(text: string) {
      controller.enqueue(enc.encode(text));
    },
    end() {
      controller.close();
    },
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("createSSEParser", () => {
  it("dispatches typed events and ignores heartbeat comments", () => {
    const got: Array<[string, string]> = [];
    const push = createSSEParser((type, data) => got.push([type, data]));
    push('event: change\ndata: {"hash": "h1"}\n\n: heartbeat\n\n');
    push('event: change\ndata: {"hash": "h2"}\n\n');
    expect(got).toEqual([
      ["change", '{"hash": "h1"}'],
      ["change", '{"hash": "h2"}'],
    ]);
  });

  it("reassembles events split across arbitrary chunk boundaries", () => {
    const got: Array<[string, string]> = [];
    const push = createSSEParser((type, data) => got.push([type, data]));
    const wire = 'event: change\ndata: {"hash": "h1"}\n\n';
    for (const ch of wire) push(ch);
    expect(got).toEqual([["change", '{"hash": "h1"}']]);
  });

  it("defaults the event type to message and handles retry fields silently", () => {
    const got: Array<[string, string]> = [];
    const push = createSSEParser((type, data) => got.push([type, data]));
    push("retry: 3000\n\ndata: plain\n\n");
    expect(got).toEqual([["message", "plain"]]);
  });

  it("joins multiple data lines of one event with newlines", () => {
    const got: Array<[string, string]> = [];
    const push = createSSEParser((type, data) => got.push([type, data]));
    push("event: change\ndata: first\ndata: second\n\n");
    expect(got).toEqual([["change", "first\nsecond"]]);
  });

  it("strips exactly one leading space and keeps significant whitespace", () => {
    const got: Array<[string, string]> = [];
    const push = createSSEParser((type, data) => got.push([type, data]));
    push("data:no-space\n\ndata:  two spaces \n\n");
    expect(got).toEqual([
      ["message", "no-space"],
      ["message", " two spaces "],
    ]);
  });

  it("handles CRLF line endings", () => {
    const got: Array<[string, string]> = [];
    const push = createSSEParser((type, data) => got.push([type, data]));
    push("event: change\r\ndata: payload\r\n\r\n");
    expect(got).toEqual([["change", "payload"]]);
  });
});

describe("watchView", () => {
  it("invokes onChange per change event and never for heartbeats", async () => {
    const feed = feedableStream();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(feed.stream, {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const onChange = vi.fn();
    const handle = watchView("component-reachability", [], onChange);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const url = String(fetchMock.mock.calls[0][0]);
    expect(url).toContain("/views/component-reachability:watch");

    feed.feed('event: change\ndata: {"hash": "h1"}\n\n');
    await vi.waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    feed.feed(": heartbeat\n\n");
    feed.feed('event: change\ndata: {"hash": "h2"}\n\n');
    await vi.waitFor(() => expect(onChange).toHaveBeenCalledTimes(2));
    handle.close();
  });

  it("passes params through to the watch URL", async () => {
    const feed = feedableStream();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(feed.stream, { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const handle = watchView("sample-history", ["owner=disp-1", "key=icmp.rtt_avg"], vi.fn());
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const url = String(fetchMock.mock.calls[0][0]);
    expect(url).toContain("param=owner%3Ddisp-1");
    expect(url).toContain("param=key%3Dicmp.rtt_avg");
    handle.close();
  });

  it("stops and raises the session-ended signal on 401", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 401 }));
    vi.stubGlobal("fetch", fetchMock);
    const bounced = vi.fn();
    setUnauthorizedHandler(bounced);

    watchView("component-reachability", [], vi.fn(), { retryMs: 1 });
    await vi.waitFor(() => expect(bounced).toHaveBeenCalledTimes(1));
    // No reconnect: a dead session must not leave a poll running behind the
    // login redirect.
    await new Promise((r) => setTimeout(r, 30));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([403, 404])("stops without retrying on a permanent %i", async (status) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status }));
    vi.stubGlobal("fetch", fetchMock);
    watchView("component-reachability", [], vi.fn(), { retryMs: 1 });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await new Promise((r) => setTimeout(r, 30));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("backs off between repeated connect failures", async () => {
    // Every attempt fails, so the delay doubles off the base. With a 20ms base
    // three attempts cost at least 20 + 40 (less the jitter floor of 0.8), far
    // more than the fixed-delay 40 a non-backing-off client would take.
    const fetchMock = vi.fn().mockRejectedValue(new Error("down"));
    vi.stubGlobal("fetch", fetchMock);
    const started = Date.now();
    const handle = watchView("component-reachability", [], vi.fn(), { retryMs: 20 });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3), { timeout: 2000 });
    handle.close();
    expect(Date.now() - started).toBeGreaterThanOrEqual(0.8 * (20 + 40));
  });

  it("reconnects after the stream drops and stops after close()", async () => {
    const first = feedableStream();
    const second = feedableStream();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(first.stream, { status: 200 }))
      .mockResolvedValueOnce(new Response(second.stream, { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const onChange = vi.fn();
    const handle = watchView("component-reachability", [], onChange, { retryMs: 1 });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    first.feed('event: change\ndata: {"hash": "h1"}\n\n');
    await vi.waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));

    // The server drops the stream: the client reconnects and the fresh
    // baseline event lands.
    first.end();
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    second.feed('event: change\ndata: {"hash": "h1"}\n\n');
    await vi.waitFor(() => expect(onChange).toHaveBeenCalledTimes(2));

    // close() ends the loop: no further connects even if the stream ends.
    handle.close();
    second.end();
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
