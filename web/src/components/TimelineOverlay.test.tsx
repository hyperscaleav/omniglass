import { afterEach, describe, expect, it, vi } from "vitest";
import { uuidFor } from "../lib/testids";
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Router, Route } from "@solidjs/router";
import { createSignal } from "solid-js";
import TimelineOverlay from "./TimelineOverlay";
import TimeseriesChart from "./TimeseriesChart";
import { CursorProvider, createCursor } from "../lib/cursor";
import type { TimeRange } from "../lib/time_range";

// The overlay timeline (#2425): one component's or system's history on one
// axis, read over the page's range. Tested on its own against the real reads,
// fetch stubbed at the wire.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const MIN = 60_000;
const now = Date.now();
const ago = (m: number) => new Date(now - m * MIN).toISOString();

type Read = { path: string; search: URLSearchParams; body?: { bindings: { primitive: string; from?: string; to?: string; keys?: string[] }[] } };

function stub(reads: Read[], permissions: string[] = ["*:*"]) {
  const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { "content-type": "application/json" } });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const req = input instanceof Request ? input : new Request(String(input));
      const u = new URL(req.url);
      const path = u.pathname.replace(/^\/api\/v1/, "");
      const body = req.method === "POST" ? await req.clone().json() : undefined;
      reads.push({ path, search: u.searchParams, body });
      if (path === "/components/disp/health")
        return json({ owner: "disp", owner_kind: "component", health: "healthy", carried_in: "healthy", slots: [], systems: [], transitions: [{ ts: ago(40), health: "outage" }, { ts: ago(26), health: "healthy" }] });
      if (path === "/components/disp/alarms")
        return json({
          component: "disp",
          health: "healthy",
          alarms: [
            { id: "a1", component: "disp", component_id: "disp", severity: "high", message: "Reachability lost", raised_by_rule: "reachability-lost", raised_by_rule_id: uuidFor("tl-rule-1"), raised_at: ago(40), cleared_at: ago(26), acknowledged: false, active: false, dedup_key: "k" },
          ],
        });
      if (path === "/components/disp/commands")
        return json({ commands: [{ id: 7, command_type: "power", component: "disp", component_id: "disp", status: "settled", phase: "settled", issued_at: ago(30), deadline: ago(29), delivery_count: 1 }] });
      if (path === "/components/disp/alarms/a1:timeline") return json({ entries: [{ at: ago(40), kind: "audit", what: "raised" }] });
      if (path === "/auth/me") return json({ username: "dev", permissions });
      if (path === "/event-rules") return json({ event_rules: [{ id: uuidFor("tl-rule-1"), name: "reachability-lost", sample_lane: "property", sample_key: "endpoint-reachable" }] });
      if (path === "/components/disp/properties") return json({ properties: [{ property_type_name: "power", property_type_id: "p", data_type: "string", from_contract: true, is_set: false, required: false }] });
      if (path === "/components/disp/metrics") return json({ metrics: [] });
      if (path === "/severity-levels") return json({ severity_levels: [{ name: "high", label: "High", color: "red", rank: 4, outage: true }] });
      if (path === "/views:run") {
        const b = body!.bindings[0];
        const result = { covered_from: ago(10000), cursor: "b.1", entities: [], from: b.from, to: b.to } as Record<string, unknown>;
        if (b.primitive === "event") result.events = [{ id: 1, entity: "disp", key: "power-changed", message: "power off", origin: "caught", ts: ago(41) }];
        if (b.primitive === "log") result.logs = [
            { id: 2, entity: "disp", message: "link down", severity: "error", ts: ago(39) },
            // A device clock hours behind: read by arrival, so it sits at its arrival.
            { id: 3, entity: "disp", message: "ntp drift", severity: "warning", ts: ago(500), received_at: ago(20) },
          ];
        const series: Record<string, unknown> = {
          power: { entity: "disp", key: "power", instance: "", carried_in: "on", points: [{ ts: ago(41), value: "off" }, { ts: ago(28), value: "on" }] },
          "endpoint-reachable": { entity: "disp", key: "endpoint-reachable", instance: "snmp", carried_in: "up", points: [{ ts: ago(40), value: "down" }, { ts: ago(26), value: "up" }] },
        };
        if (b.primitive === "property") result.property = (b.keys ?? []).map((k: string) => series[k]).filter(Boolean);
        return json({ cursor: "b.1", now: new Date(now).toISOString(), results: [result] });
      }
      return json({});
    }),
  );
}

function mount(range: () => TimeRange, extra?: () => unknown) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const cursor = createCursor();
  render(() => (
    <Router
      root={() => (
        <QueryClientProvider client={qc}>
          <CursorProvider cursor={cursor}>
            <TimelineOverlay kind="components" id="disp" range={range()} />
            {extra?.() as never}
          </CursorProvider>
        </QueryClientProvider>
      )}
    >
      <Route path="*" component={() => null} />
    </Router>
  ));
  return cursor;
}

const hour: TimeRange = { kind: "preset", preset: "1h" };

describe("TimelineOverlay", () => {
  it("draws a raised-then-cleared alarm whose hover gives raised at, cleared at and how long, over the degraded health", async () => {
    stub([]);
    mount(() => hour);
    const span = await screen.findByTestId("timeline-alarm-span");
    const tip = span.getAttribute("aria-label")!;
    expect(tip).toMatch(/raised .*cleared .*raised for 14m/);
    expect(tip).toContain("reachability-lost");
    fireEvent.mouseEnter(span);
    expect(screen.getByTestId("timeline-readout").textContent).toContain("raised for 14m");
    // The health lane holds the outage the alarm made, between two healthy stretches.
    const health = screen.getByTestId("timeline-health-row");
    const titles = [...health.querySelectorAll("title")].map((t) => t.textContent);
    expect(titles.some((t) => t?.startsWith("outage from") && t.includes("held 14m"))).toBe(true);
    expect(titles.filter((t) => t?.startsWith("healthy")).length).toBe(2);
  });

  it("lays the command, the event, the log line and the watched property on the same axis", async () => {
    stub([]);
    mount(() => hour);
    await waitFor(() => expect(screen.getByTestId("timeline-commands-row").querySelector("[role=button]")).toBeTruthy());
    expect(screen.getByTestId("timeline-commands-row").querySelector("[role=button]")!.getAttribute("aria-label")).toContain("power on disp · settled");
    await waitFor(() => expect(screen.getByTestId("timeline-events-row").querySelector("[role=button]")?.getAttribute("aria-label")).toContain("power off"));
    await waitFor(() => expect(screen.getByTestId("timeline-logs-row").querySelectorAll("[role=button]")).toHaveLength(2));
    const logs = [...screen.getByTestId("timeline-logs-row").querySelectorAll("[role=button]")].map((b) => b.getAttribute("aria-label"));
    expect(logs[0]).toContain("link down");
    expect(logs[1]).toMatch(/ntp drift · .*, arrived /);
    // The alarming rule watches endpoint-reachable (no declared property, but
    // read all the same), and power changed: both are shown unasked.
    await waitFor(() => expect(screen.getAllByTestId("timeline-property-row")).toHaveLength(2));
    const [reach, power] = screen.getAllByTestId("timeline-property-row");
    expect(reach.textContent).toContain("endpoint-reachable [snmp]");
    expect([...reach.querySelectorAll("title")].map((t) => t.textContent?.split(" ")[0])).toEqual(["up", "down", "up"]);
    expect([...power.querySelectorAll("title")].map((t) => t.textContent?.split(" ")[0])).toEqual(["on", "off", "on"]);
  });

  it("opens a command's arc and an alarm's history from their marks", async () => {
    stub([]);
    mount(() => hour);
    await waitFor(() => expect(screen.getByTestId("timeline-commands-row").querySelector("[role=button]")).toBeTruthy());
    fireEvent.click(screen.getByTestId("timeline-commands-row").querySelector("[role=button]")!);
    expect(screen.getByText("Open its arc").getAttribute("href")).toBe("/components/disp?tab=activity&command=7");
    fireEvent.click(screen.getByTestId("timeline-alarm-span"));
    await waitFor(() => expect(screen.getByTestId("timeline-detail").textContent).toContain("raised"));
  });

  it("crosses every lane with the shared cursor, and the metric charts beside it", async () => {
    stub([]);
    const cursor = mount(
      () => hour,
      () => <TimeseriesChart samples={[{ ts: ago(30), value: 1 }, { ts: ago(10), value: 2 }]} now={now} windowMs={60 * MIN} />,
    );
    await screen.findByTestId("timeline-alarm-span");
    await waitFor(() => expect(screen.getAllByTestId("timeline-property-row")).toHaveLength(2));
    cursor.set(now - 33 * MIN);
    await waitFor(() => expect(screen.getAllByTestId("timeline-cursor").length).toBeGreaterThanOrEqual(5));
    expect(screen.getByTestId("cursor-line")).toBeTruthy();
    expect(screen.getByTestId("timeline-readout").dataset.at).toBe("cursor");
    const readout = screen.getByTestId("timeline-readout").textContent!;
    expect(readout).toContain("health outage");
    expect(readout).toContain("reachability-lost raised for 14m");
    expect(readout).toContain("power off");
  });

  it("re-reads every lane when the page's range changes", async () => {
    const reads: Read[] = [];
    stub(reads);
    const [range, setRange] = createSignal<TimeRange>(hour);
    mount(range);
    await screen.findByTestId("timeline-alarm-span");
    const from = Date.UTC(2026, 10, 1);
    const to = Date.UTC(2026, 10, 2);
    reads.length = 0;
    // The lanes keep what they hold while a new range reads, never blanking.
    setRange({ kind: "preset", preset: "6h" });
    expect(screen.queryByTestId("timeline-alarm-span")).toBeTruthy();
    reads.length = 0;
    setRange({ kind: "absolute", from, to });
    const since = new Date(from).toISOString();
    await waitFor(() => {
      for (const p of ["/components/disp/health", "/components/disp/alarms", "/components/disp/commands"]) {
        expect(reads.some((r) => r.path === p && r.search.get("since") === since)).toBe(true);
      }
      for (const prim of ["event", "log", "property"]) {
        expect(reads.some((r) => r.body?.bindings[0].primitive === prim && r.body.bindings[0].from === since)).toBe(true);
      }
    });
  });

  it("leaves the views-backed lanes unread, and says why, for a caller without view:read", async () => {
    const reads: Read[] = [];
    stub(reads, ["component:read"]);
    mount(() => hour);
    await screen.findByTestId("timeline-alarm-span");
    await waitFor(() => expect(screen.getByTestId("timeline-events-row").textContent).toContain("needs view:read"));
    expect(screen.getByTestId("timeline-logs-row").textContent).toContain("needs view:read");
    expect(reads.some((r) => r.path === "/views:run")).toBe(false);
  });
});
