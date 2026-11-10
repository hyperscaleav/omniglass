import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { fireEvent, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import PlatformHealth from "./PlatformHealth";
import { NOT_WIRED, PLATFORM_SIM_KEY, PLATFORM_VIEW_KEY, WARMING_UP, type PlatformSimNode, type PlatformSimulation, type PlatformView } from "../lib/platformhealth";
import { SIMULATION_KEY, type SimFleet } from "../lib/simulation";
import { NODE_DOWN_AFTER_MS } from "../lib/nodes";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Platform health page (#1644): a header (the build running, its uptime,
// the platform verdict linking to its system, a lab badge) over a node table
// whose columns are the groups the view returns. The view is seeded into the
// query cache, so no server is needed; every clock is the body's read_at.
const readAt = "2026-11-09T12:00:00Z";
const readMs = Date.parse(readAt);
const ago = (ms: number) => new Date(readMs - ms).toISOString();
const viewer: Me = { principal: { id: "v", kind: "human" }, permissions: ["*:read"], grants: [] };

const full: PlatformView = {
  read_at: readAt,
  lab: true,
  figures_read: true,
  system: { id: uuidFor("platform"), name: "omniglass-platform", verdict: "degraded" },
  servers: [{ component: uuidFor("server"), name: "lab-server", version: "v9.9.9", started_at: ago((2 * 3600 + 5 * 60) * 1000), verdict: "healthy" }],
  groups: [
    { name: "checkin", label: "Checkin", keys: [{ name: "checkin-age", label: "Check-in Age", unit: "s", description: "Seconds since the node last checked in." }] },
    { name: "commands", label: "Commands", keys: [{ name: "command-pending-count", label: "Pending Commands", unit: "", description: "Commands waiting on the node." }] },
  ],
  nodes: [
    {
      name: "edge-alpha", label: "Edge Alpha", enrolled: true, last_heartbeat_at: ago(3000),
      component: uuidFor("alpha"), verdict: "healthy",
      figures: { "checkin-age": { value: 3, observed_at: ago(3000) } },
    },
    {
      name: "edge-bravo", label: "", enrolled: true, last_heartbeat_at: ago(NODE_DOWN_AFTER_MS + 1),
      component: uuidFor("bravo"), verdict: "outage",
      figures: { "checkin-age": { value: 400, observed_at: ago(1000) }, "command-pending-count": { value: 0, observed_at: ago(1000) } },
    },
  ],
};

// A seeded view mounts on a client that never retries; a refused one (null)
// mounts on the production default, three retries with backoff, so only the
// page's own retry policy can surface the refusal promptly.
// The simulator fleet is seeded disconnected unless a test connects one, so
// the simulation section's condition is the test's to set.
const disconnected: SimFleet = { connected: false, nodes: [] };
function mount(view: PlatformView | null, sim: { me?: Me; fleet?: SimFleet; body?: PlatformSimulation } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: view ? { staleTime: Infinity, retry: false } : { staleTime: Infinity } } });
  if (view) qc.setQueryData([...PLATFORM_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], sim.me ?? viewer);
  qc.setQueryData([...SIMULATION_KEY], sim.fleet ?? disconnected);
  if (sim.body) qc.setQueryData([...PLATFORM_SIM_KEY], sim.body);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={PlatformHealth} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

// refuse answers /views/platform with the server's own 403 body, so the page's
// query runs for real through its retry policy.
function refuse() {
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(
    JSON.stringify({ title: "Forbidden", status: 403, detail: "forbidden" }),
    { status: 403, headers: { "content-type": "application/problem+json" } },
  ))));
}


describe("PlatformHealth page", () => {
  it("heads the page with the build, its uptime at read_at, the verdict linking to its system, and the lab badge", () => {
    mount(full);
    const header = screen.getByTestId("platform-header");
    expect(within(header).getByText("v9.9.9")).toBeInTheDocument();
    expect(within(header).getByText("2h 5m")).toBeInTheDocument();
    const verdict = within(header).getByTestId("platform-verdict");
    expect(verdict.closest("a")).toHaveAttribute("href", `/systems/${uuidFor("platform")}`);
    expect(verdict).toHaveTextContent("degraded");
    expect(screen.getByTestId("platform-lab")).toBeInTheDocument();
  });

  // The header teaches through a keyboard-reachable tooltip, not a title
  // attribute a tab walk never reaches: focusing the column's trigger reveals
  // its catalog description.
  it("renders one column per key, by group, each headed with its catalog description", async () => {
    mount(full);
    const table = screen.getByTestId("platform-nodes");
    expect(within(table).getByText("Checkin")).toBeInTheDocument();
    expect(within(table).getByText("Commands")).toBeInTheDocument();
    const trigger = within(table).getByRole("button", { name: "Check-in Age" });
    expect(trigger.closest("th")).not.toHaveAttribute("title");
    fireEvent.keyDown(document.body, { key: "Tab" });
    trigger.focus();
    expect(await screen.findByText("Seconds since the node last checked in.")).toBeInTheDocument();
  });

  it("gives each node a row: the pill at read_at, its figures, not reported for a missing one, and its verdict as the row's tone", () => {
    mount(full);
    const alpha = screen.getByTestId("platform-node-edge-alpha");
    expect(within(alpha).getByText("up")).toBeInTheDocument();
    expect(within(alpha).getByText("3 s")).toBeInTheDocument();
    expect(within(alpha).getByText("not reported")).toBeInTheDocument();
    const bravo = screen.getByTestId("platform-node-edge-bravo");
    expect(within(bravo).getByText("down")).toBeInTheDocument();
    expect(within(bravo).getByText("0")).toBeInTheDocument();
    expect(bravo).toHaveAttribute("data-verdict", "outage");
    expect(bravo.className).toContain("bg-error/5");
  });

  it("opens a row onto its platform component, with a secondary link to the node's blade", () => {
    mount(full);
    const alpha = screen.getByTestId("platform-node-edge-alpha");
    expect(within(alpha).getByRole("link", { name: "Edge Alpha" })).toHaveAttribute("href", `/components/${uuidFor("alpha")}`);
    expect(within(alpha).getByRole("link", { name: "Open edge-alpha in Nodes" })).toHaveAttribute("href", "/nodes?n=edge-alpha");
  });

  it("without platform reach renders the node rows and the fleet-wide-read line, with no console error", () => {
    const errors = vi.spyOn(console, "error");
    const bare: PlatformView = {
      read_at: readAt, lab: false, figures_read: false, servers: [], groups: full.groups,
      system: null as unknown as undefined,
      nodes: [{ name: "edge-alpha", label: "", enrolled: true, last_heartbeat_at: ago(1000) }, { name: "edge-new", label: "", enrolled: false }],
    };
    mount(bare);
    expect(screen.getByTestId("platform-node-edge-alpha")).toBeInTheDocument();
    expect(screen.getByTestId("platform-node-edge-new")).toBeInTheDocument();
    expect(screen.getByTestId("platform-reach")).toHaveTextContent(/fleet-wide read/);
    expect(screen.queryByTestId("platform-lab")).toBeNull();
    expect(screen.queryByTestId("platform-verdict")).toBeNull();
    expect(errors).not.toHaveBeenCalled();
  });

  // A scoped node read is refused (403): the page says which read it needs at
  // once, instead of a loading line that never resolves.
  it("on a refusal says the page needs a fleet-wide node read, with no loading line", async () => {
    refuse();
    mount(null);
    const refusal = await screen.findByRole("alert");
    expect(refusal).toHaveTextContent(/fleet-wide node read/);
    // A refusal is an answer about the caller's read, not a fault: warning, not error.
    expect(refusal.className).toContain("alert-warning");
    expect(refusal.className).not.toContain("alert-error");
    expect(screen.queryByText(/Loading the platform/)).toBeNull();
    expect(screen.queryByTestId("platform-nodes")).toBeNull();
  });
});

// With a simulator (#1647): the section keys on the nav's "simulator"
// condition, a simulation reader with a simulator connected, never on lab.
const connected: SimFleet = { connected: true, nodes: [] };
const measured: PlatformSimNode = {
  node: "edge-alpha", state: "measured", start: ago(600_000), end: ago(60_000), brackets: 11, error: "",
  gaps: [
    { gap: "connect-not-accepted", protocol: "tcp", count: 0, in: 40, ratio: 0, masked: 0, masked_by: "connections still in the kernel backlog at the end read", answered_late: 0, reading: "loss" },
    { gap: "sent-not-received", protocol: "http", count: 30, in: 120, ratio: 0.25, masked: 0, masked_by: "requests in flight at the node's end read", answered_late: 0, reading: "impaired" },
    { gap: "published-not-landed", protocol: "", count: 2, in: 40, ratio: 0.05, masked: 1, masked_by: "receipts landed in the margin before the window", answered_late: 0, reading: "loss" },
    { gap: "sent-not-received", protocol: "icmp", count: 0, in: 0, ratio: null, masked: 0, masked_by: "", answered_late: 0, reading: "not observed by the sim" },
  ],
  receipts: { landed: 42, margin: 1, backlog: 0, after_end: 0 },
  protocols: [{ protocol: "http", received: 90, answered: 90, latency_ms: { p50: 1, p95: 2.5, p99: 4, n: 90 }, poll_targets: 1, in_flight_high: 2 }],
  in_flight_high: 2, queue_depth: null, busy_workers: null,
  devices: [{ component_id: uuidFor("display"), name: "lobby-display", received: { http: 90 }, impaired: true, unhit: false }],
};
const simBody = (nodes: PlatformSimNode[]): PlatformSimulation => ({ read_at: readAt, window_seconds: 600, bracket_seconds: 60, nodes });

describe("PlatformHealth with a simulator", () => {
  it("is absent with no simulator connected, even on a lab instance", () => {
    mount(full, { body: simBody([measured]) });
    expect(screen.queryByTestId("platform-simulation")).toBeNull();
  });

  it("is absent for a principal who cannot read simulation", () => {
    const nodeReader: Me = { principal: { id: "n", kind: "human" }, permissions: ["node:read", "component:read"], grants: [] };
    mount(full, { me: nodeReader, fleet: connected, body: simBody([measured]) });
    expect(screen.queryByTestId("platform-simulation")).toBeNull();
  });

  it("is present with a simulator connected: gaps worst first, each read by tone and naming its masking term", () => {
    mount({ ...full, lab: false }, { fleet: connected, body: simBody([measured]) });
    const section = screen.getByTestId("platform-simulation");
    expect(within(section).getByText(/last 10 minutes/)).toBeInTheDocument();
    const rows = within(section).getAllByTestId("platform-sim-gap");
    expect(rows.map((r) => r.getAttribute("data-ratio"))).toEqual(["0.25", "0.05", "0", ""]);
    expect(rows.map((r) => r.getAttribute("data-tone"))).toEqual(["expected", "fault", "calm", "calm"]);
    expect(rows[0]).toHaveTextContent("25.0%");
    expect(rows[1]).toHaveTextContent("1 in receipts landed in the margin before the window");
    expect(rows[2]).toHaveTextContent("connections still in the kernel backlog at the end read");
    expect(rows[2].className).toBe("");
  });

  it("links each device to its drawer on the Simulation page and names the figures not wired yet", () => {
    mount(full, { fleet: connected, body: simBody([measured]) });
    const device = screen.getByTestId("platform-sim-device");
    expect(within(device).getByRole("link", { name: "lobby-display" })).toHaveAttribute("href", `/simulation?c=${uuidFor("display")}`);
    expect(within(device).getByText("impaired")).toBeInTheDocument();
    expect(screen.getByTestId("platform-sim-protocol")).toHaveAttribute("data-received", "90");
    expect(screen.getAllByText(NOT_WIRED)).toHaveLength(2);
  });

  it("while warming up says why and shows no gap and no number", () => {
    const warming: PlatformSimNode = { ...measured, state: "warming up", start: null, end: null, brackets: 1, gaps: null, protocols: [], devices: [] };
    mount(full, { fleet: connected, body: simBody([warming]) });
    const card = screen.getByTestId("platform-sim-edge-alpha");
    expect(within(card).getByTestId("platform-sim-note")).toHaveTextContent(WARMING_UP);
    expect(within(card).queryByTestId("platform-sim-gaps")).toBeNull();
    expect(within(card).getByText("Batches landed").closest("div")!.parentElement).toHaveTextContent("-");
    expect(within(card).queryByText("42")).toBeNull();
  });
});
