import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@solidjs/testing-library";
import { createEffect } from "solid-js";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import MetricsGrid from "./MetricsGrid";

const seen: number[] = [];
vi.mock("./TimeseriesChart", () => ({
  default: (p: { now: number }) => {
    createEffect(() => seen.push(p.now));
    return <div data-testid="chart" />;
  },
}));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  seen.length = 0;
});

// A preset window slides but its query key is fixed, so each series poll
// refetches under one key; the chart's right edge must follow the read, not the
// minute clock, or a poll past the last tick draws buckets clamped at the edge.
describe("MetricsGrid preset right edge", () => {
  it("moves the chart's now with each series read, between clock ticks", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const json = (b: unknown) => Promise.resolve(new Response(JSON.stringify(b), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const req = input instanceof Request ? input : new Request(String(input));
        const path = new URL(req.url).pathname.replace(/^\/api\/v1/, "");
        if (path === "/systems/room-1/metrics")
          return json({ metrics: [{ metric_type_name: "rtt", label: "RTT", unit: "ms", value: 3, is_sampled: true, from_contract: true, instance: "" }] });
        return json({ buckets: [{ ts: new Date().toISOString(), count: 1, min: 1, max: 1, avg: 1, last: 1 }] });
      }),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(() => (
      <QueryClientProvider client={qc}>
        <MetricsGrid kind="systems" id="room-1" range={{ kind: "preset", preset: "24h" }} />
      </QueryClientProvider>
    ));
    await waitFor(() => expect(seen.length).toBeGreaterThan(0));
    const first = seen[seen.length - 1];
    vi.setSystemTime(new Date("2026-01-01T00:00:20Z"));
    await qc.refetchQueries({ queryKey: ["metric-buckets"] });
    await waitFor(() => expect(seen[seen.length - 1]).toBeGreaterThan(first));
    expect(seen[seen.length - 1]).toBe(Date.now());
  });
});
