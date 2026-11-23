import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { createSignal } from "solid-js";
import MetricsGrid from "./MetricsGrid";
import type { TimeRange } from "../lib/time_range";

// The metrics grid (#2424): one card per effective metric of a component or a
// system over the page's range, every chart on one shared cursor. It takes
// plain props and holds no page logic, so it is tested on its own, against the
// real reads (fetch stubbed at the wire).
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const HOUR = 3600_000;
const metrics = [
  { metric_type_name: "cpu-load", label: "CPU load", unit: "percent", value: 42.5, is_sampled: true, from_contract: true, instance: "" },
  { metric_type_name: "rtt", label: "Round trip", unit: "ms", value: 3, is_sampled: true, from_contract: true, instance: "" },
  { metric_type_name: "fan-speed", label: "Fan speed", value: 0, is_sampled: false, from_contract: true, instance: "" },
];

// Buckets ending now, one every `res` seconds, the last few hours filled.
function buckets(hours: number, res: number, base: number) {
  const now = Date.now();
  const n = Math.floor((hours * HOUR) / (res * 1000));
  const start = Math.floor((now - hours * HOUR) / (res * 1000)) * res * 1000;
  return Array.from({ length: n }, (_, i) => {
    const v = base + (i % 7);
    return { ts: new Date(start + i * res * 1000).toISOString(), count: 1, min: v, max: v, avg: v, last: v };
  });
}

function stub(reads: string[]) {
  const json = (b: unknown) => Promise.resolve(new Response(JSON.stringify(b), { status: 200, headers: { "content-type": "application/json" } }));
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL) => {
      const req = input instanceof Request ? input : new Request(String(input));
      const u = new URL(req.url);
      const path = u.pathname.replace(/^\/api\/v1/, "");
      reads.push(path + u.search);
      if (path === "/components/cam-1/metrics" || path === "/systems/room-1/metrics") return json({ metrics });
      const m = path.match(/\/metrics\/([^/]+)\/samples$/);
      if (m) {
        const hours = Number(u.searchParams.get("hours"));
        const res = Number(u.searchParams.get("resolution"));
        if (m[1] === "fan-speed") return json({ buckets: [] });
        return json({ buckets: buckets(hours, res, m[1] === "cpu-load" ? 40 : 2) });
      }
      return json({});
    }),
  );
}

let lastClient: QueryClient | undefined;

function mount(kind: "components" | "systems", id: string, range: () => TimeRange) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  lastClient = qc;
  return render(() => (
    <QueryClientProvider client={qc}>
      <MetricsGrid kind={kind} id={id} range={range()} />
    </QueryClientProvider>
  ));
}

describe("MetricsGrid", () => {
  it("draws one card per effective metric, a metric with no data in range included and marked", async () => {
    stub([]);
    mount("components", "cam-1", () => ({ kind: "preset", preset: "6h" }));
    await waitFor(() => expect(screen.getAllByTestId("timeseries-chart")).toHaveLength(2));
    const cards = screen.getAllByTestId(/^metric-card-/);
    expect(cards.map((c) => c.dataset.testid)).toEqual(["metric-card-cpu-load", "metric-card-rtt", "metric-card-fan-speed"]);
    const cpu = screen.getByTestId("metric-card-cpu-load");
    expect(within(cpu).getByText("CPU load")).toBeTruthy();
    expect(within(cpu).getByText("42.5")).toBeTruthy();
    expect(within(cpu).getByText("percent")).toBeTruthy();
    const fan = screen.getByTestId("metric-card-fan-speed");
    expect(within(fan).getByText("No data in range")).toBeTruthy();
    expect(within(fan).queryByTestId("timeseries-chart")).toBeNull();
  });

  // A sampled metric names its instance (here the owner-level "").
  const read = (reads: string[], prefix: string) => reads.some((r) => r.startsWith(prefix));

  it("re-reads every series when the page range changes", async () => {
    const reads: string[] = [];
    stub(reads);
    const [range, setRange] = createSignal<TimeRange>({ kind: "preset", preset: "24h" });
    mount("systems", "room-1", range);
await waitFor(() => expect(read(reads, "/systems/room-1/metrics/rtt/samples?hours=24&resolution=300")).toBe(true));
    setRange({ kind: "preset", preset: "1h" });
    await waitFor(() => expect(read(reads, "/systems/room-1/metrics/rtt/samples?hours=1&resolution=60")).toBe(true));
    await waitFor(() => expect(read(reads, "/systems/room-1/metrics/fan-speed/samples?hours=1&resolution=60")).toBe(true));
  });

  it("an absolute range reads its own from and to through the views binding, and draws that window", async () => {
    const bodies: { bindings: { keys: string[]; from: string; to: string; step_seconds: number }[] }[] = [];
    const to = Date.now() - 2 * HOUR;
    const from = to - 3 * HOUR;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const req = input instanceof Request ? input : new Request(String(input));
        const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { "content-type": "application/json" } });
        if (req.method === "POST") {
          const body = await req.clone().json();
          bodies.push(body);
          const v = 5;
          const series = [{ instance: "", buckets: [{ ts: new Date(from + HOUR).toISOString(), count: 1, min: v, max: v, avg: v, last: v }] }];
          return json({ results: [{ metric: series, cursor: "b.1", covered_from: "", entities: [], from: "", to: "" }], cursor: "b.1" });
        }
        return json({ metrics: [metrics[1]] });
      }),
    );
    mount("systems", "room-1", () => ({ kind: "absolute", from, to }));
    await waitFor(() => expect(screen.getAllByTestId("timeseries-chart")).toHaveLength(1));
    expect(bodies[0].bindings[0]).toMatchObject({ keys: ["rtt"], from: new Date(from).toISOString(), to: new Date(to).toISOString(), step_seconds: 60 });
  });

  it("hovering one chart puts the crosshair at the same instant on every chart in the grid; leaving clears all", async () => {
    stub([]);
    mount("components", "cam-1", () => ({ kind: "preset", preset: "6h" }));
    await waitFor(() => expect(screen.getAllByTestId("timeseries-chart")).toHaveLength(2));
    const [a] = screen.getAllByTestId("timeseries-chart");
    (a as unknown as SVGSVGElement).getBoundingClientRect = () => ({ left: 0, top: 0, width: 640, height: 170, right: 640, bottom: 170, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
    fireEvent.mouseMove(a, { clientX: 320 });
    const hairs = screen.getAllByTestId("chart-crosshair");
    expect(hairs).toHaveLength(2);
    expect(hairs.every((h) => h.querySelector("[data-testid=cursor-value]"))).toBe(true);
    const x = (h: Element) => h.querySelector("[data-testid=cursor-line]")!.getAttribute("x1");
    expect(x(hairs[1])).toBe(x(hairs[0]));
    fireEvent.mouseLeave(a);
    expect(screen.queryAllByTestId("chart-crosshair")).toHaveLength(0);
  });

  it("a series that fails to read says so on its card and offers a retry", async () => {
    let fail = true;
    const json = (b: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(b), { status, headers: { "content-type": "application/json" } }));
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const u = new URL((input instanceof Request ? input : new Request(String(input))).url);
        if (u.pathname.endsWith("/metrics")) return json({ metrics: [metrics[0]] });
        if (fail) return json({ title: "boom" }, 500);
        return json({ buckets: buckets(6, 60, 40) });
      }),
    );
    mount("components", "cam-1", () => ({ kind: "preset", preset: "6h" }));
    const card = await screen.findByTestId("metric-card-cpu-load");
    expect(await within(card).findByText("Could not read this series.")).toBeTruthy();
    fail = false;
    fireEvent.click(within(card).getByRole("button", { name: "Retry" }));
    expect(await within(card).findByTestId("timeseries-chart")).toBeTruthy();
  });

  // The metrics list polls, and each poll hands back fresh objects: a card is
  // kept by its metric's name, so a focused chart survives the poll.
  it("keeps each card, and a focused chart's focus, across a poll of the metric list", async () => {
    stub([]);
    mount("components", "cam-1", () => ({ kind: "preset", preset: "6h" }));
    await waitFor(() => expect(screen.getAllByTestId("timeseries-chart")).toHaveLength(2));
    const [chart] = screen.getAllByTestId("timeseries-chart");
    (chart as unknown as SVGSVGElement).focus();
    expect(document.activeElement).toBe(chart);
    await lastClient!.refetchQueries({ queryKey: ["component-metrics", "cam-1"] });
    expect(screen.getAllByTestId("timeseries-chart")[0]).toBe(chart);
    expect(document.activeElement).toBe(chart);
  });

  // A link can carry an absolute range to a caller who may not read views
  // (the picker offers none to them): the card says what is missing rather
  // than offering a Retry that cannot succeed.
  it("an absolute range refused for want of view:read says so, with no Retry", async () => {
    const to = Date.now() - HOUR;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const req = input instanceof Request ? input : new Request(String(input));
        const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "content-type": "application/json" } });
        if (req.method === "POST") return json({ status: 403, title: "Forbidden", detail: "view:read" }, 403);
        return json({ metrics: [metrics[1]] });
      }),
    );
    mount("systems", "room-1", () => ({ kind: "absolute", from: to - HOUR, to }));
    const card = await screen.findByTestId("metric-card-rtt");
    expect(await within(card).findByText(/custom range needs the view:read permission/)).toBeTruthy();
    expect(within(card).queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("a preset range's 403 is not blamed on view:read: it reads as a failed series with Retry", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const u = new URL((input instanceof Request ? input : new Request(String(input))).url);
        const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "content-type": "application/json" } });
        if (u.pathname.endsWith("/samples")) return json({ status: 403, title: "Forbidden" }, 403);
        return json({ metrics: [metrics[1]] });
      }),
    );
    mount("systems", "room-1", () => ({ kind: "preset", preset: "6h" }));
    const card = await screen.findByTestId("metric-card-rtt");
    expect(await within(card).findByText("Could not read this series.")).toBeTruthy();
    expect(within(card).queryByText(/view:read/)).toBeNull();
    expect(within(card).getByRole("button", { name: "Retry" })).toBeTruthy();
  });

  it("draws nothing when the entity has no metrics", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({ metrics: [] }), { status: 200, headers: { "content-type": "application/json" } }))));
    mount("components", "cam-1", () => ({ kind: "preset", preset: "6h" }));
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByTestId("metrics-grid")).toBeNull();
  });
});
