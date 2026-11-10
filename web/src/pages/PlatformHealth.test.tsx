import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import PlatformHealth from "./PlatformHealth";
import { NOT_WIRED, PLATFORM_SIM_KEY, PLATFORM_VIEW_KEY, WARMING_UP, type PlatformServer, type PlatformSimNode, type PlatformSimulation, type PlatformView } from "../lib/platformhealth";
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
  servers: [{ component: uuidFor("server"), name: "lab-server", version: "v9.9.9", started_at: ago((2 * 3600 + 5 * 60) * 1000), verdict: "healthy", groups: [], alarms: [] }],
  groups: [
    { name: "checkin", label: "Checkin", keys: [{ name: "checkin-age", label: "Check-in Age", unit: "s", description: "Seconds since the node last checked in." }] },
    { name: "commands", label: "Commands", keys: [{ name: "command-pending-count", label: "Pending Commands", unit: "", description: "Commands waiting on the node." }] },
  ],
  nodes: [
    {
      name: "edge-alpha", label: "Edge Alpha", enrolled: true, last_heartbeat_at: ago(3000),
      component: uuidFor("alpha"), verdict: "healthy", loss: 0.03,
      figures: { "checkin-age": { value: 3, observed_at: ago(3000) } },
    },
    {
      name: "edge-bravo", label: "", enrolled: true, last_heartbeat_at: ago(NODE_DOWN_AFTER_MS + 1),
      component: uuidFor("bravo"), verdict: "outage",
      figures: { "checkin-age": { value: 400, observed_at: ago(1000) }, "command-pending-count": { value: 0, observed_at: ago(1000) } },
    },
  ],
  loss_hops: ["node"],
  pipeline: {
    keys: [{ name: "delivery-loss-ratio", label: "Delivery Loss", unit: "", description: "The share of what went in that did not come out." }],
    hops: [
      {
        id: "node", label: "Node", description: "The node's spool.", component_class: "node",
        keys: ["delivery-in-count", "delivery-out-count", "delivery-loss-ratio"],
        kinds: [{ kind: "metric", newest: { in: 10, out: 10, loss: 0 }, last_hour: { in: 100, out: 97, loss: 0.03 } }],
        total: { newest: { in: 10, out: 10, loss: 0 }, last_hour: { in: 100, out: 97, loss: 0.03 } },
        by_component: [],
      },
    ],
    end_to_end: [{ kind: "metric", newest: 0, last_hour: 0.03 }],
    end_to_end_total: { newest: 0, last_hour: 0.03 },
  },
};

// A server's four panels (#1645), as the view returns them: the catalog facts
// per key and the latest figure per key and instance, a ranked key's already
// cut and ordered by the server.
const SLOW = "GET /api/v1/components/{name} 2xx";
const FAST = "GET /api/v1/fast 2xx";
function panels(figures: boolean): PlatformServer["groups"] {
  const f = (key: string, instance: string, value: number) => (figures ? [{ key, instance, value, observed_at: ago(1000) }] : []);
  return [
    {
      name: "requests", label: "Requests",
      keys: [
        { name: "http-request-duration-max", label: "Slowest Request", unit: "ms", description: "The slowest request in the window, per route.", rank: 10 },
        { name: "db-pool-acquired", label: "Pool In Use", description: "Connections in use.", of: "db-pool-max" },
        { name: "db-pool-max", label: "Pool Size", description: "The pool's size." },
      ],
      figures: [...f("http-request-duration-max", SLOW, 812), ...f("http-request-duration-max", FAST, 3), ...f("db-pool-acquired", "", 4), ...f("db-pool-max", "", 20)],
    },
    { name: "ingest", label: "Ingest", keys: [{ name: "ingest-lag", label: "Ingest Lag", unit: "s", description: "How far ingest runs behind." }], figures: f("ingest-lag", "", 2.5) },
    { name: "queues", label: "Queues", keys: [{ name: "outbox-queued-depth", label: "Outbox Queued", description: "Effects waiting." }], figures: f("outbox-queued-depth", "", 0) },
    { name: "retention", label: "Retention", keys: [{ name: "retention-behind", label: "Retention Behind", unit: "d", description: "Days past due." }], figures: [...f("retention-behind", "event", 0), ...f("retention-behind", "metric", 1)] },
  ];
}

const twoServers = (): PlatformView => ({
  ...full,
  servers: [
    { component: uuidFor("server"), name: "lab-server", version: "v9.9.9", started_at: ago(1000), verdict: "healthy", groups: panels(true), alarms: [] },
    { component: uuidFor("server-b"), name: "lab-server-b", version: "v9.9.9", started_at: ago(1000), verdict: "healthy", groups: panels(true), alarms: [] },
  ],
});

// emptySeries answers every sparkline read with an empty window, so a mounted
// page's series queries resolve without a server.
function emptySeries() {
  const fetchSpy = vi.fn((_req: Request) => Promise.resolve(new Response(
    JSON.stringify({ owner: "lab-server", metric: "x", samples: [], buckets: [], covered_from: readAt }),
    { status: 200, headers: { "content-type": "application/json" } },
  )));
  vi.stubGlobal("fetch", fetchSpy);
  return fetchSpy;
}

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
  // Under the console's own base, so an in-app link proves it routes there.
  window.history.pushState({}, "", "/web/platform");
  const r = render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="*" component={PlatformHealth} />
      </Router>
    </QueryClientProvider>
  ));
  return Object.assign(r, { qc });
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
    expect(verdict.closest("a")).toHaveAttribute("href", `/web/systems/${uuidFor("platform")}`);
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

  // Where work is lost (#1646): the pipeline sits above the node table, and
  // each node's Loss group is its own hops' composed ratio, headed with a
  // tooltip that names those hops.
  it("draws the pipeline above the node table", () => {
    mount(full);
    const pipeline = screen.getByTestId("pipeline");
    expect(within(pipeline).getByTestId("pipeline-hop-node")).toBeInTheDocument();
    expect(within(pipeline).getByTestId("pipeline-end-to-end")).toHaveTextContent("3%");
  });

  it("shows each node's composed loss under a Loss group, not reported for a node with none", async () => {
    mount(full);
    const table = screen.getByTestId("platform-nodes");
    expect(within(table).getByText("Loss")).toBeInTheDocument();
    const alpha = screen.getByTestId("platform-node-edge-alpha");
    const alphaLoss = alpha.querySelector('[data-figure="loss"]')!;
    expect(alphaLoss).toHaveTextContent("3%");
    expect(alphaLoss.className).toContain("text-error");
    const bravoLoss = screen.getByTestId("platform-node-edge-bravo").querySelector('[data-figure="loss"]')!;
    expect(bravoLoss).toHaveTextContent("not reported");
    const trigger = within(table).getByRole("button", { name: "Node Loss" });
    fireEvent.keyDown(document.body, { key: "Tab" });
    trigger.focus();
    expect(await screen.findByText(/own node hop took in over the last hour/)).toBeInTheDocument();
  });

  it("opens a row onto its platform component, with a secondary link to the node's blade", () => {
    mount(full);
    const alpha = screen.getByTestId("platform-node-edge-alpha");
    expect(within(alpha).getByRole("link", { name: "Edge Alpha" })).toHaveAttribute("href", `/web/components/${uuidFor("alpha")}`);
    expect(within(alpha).getByRole("link", { name: "Open edge-alpha in Nodes" })).toHaveAttribute("href", "/web/nodes?n=edge-alpha");
  });

  it("without platform reach renders the node rows and the fleet-wide-read line, with no console error", () => {
    const errors = vi.spyOn(console, "error");
    const bare: PlatformView = {
      read_at: readAt, lab: false, figures_read: false, servers: [], groups: full.groups,
      system: null as unknown as undefined,
      nodes: [{ name: "edge-alpha", label: "", enrolled: true, last_heartbeat_at: ago(1000) }, { name: "edge-new", label: "", enrolled: false }],
      loss_hops: ["node"],
    };
    mount(bare);
    expect(screen.getByTestId("platform-node-edge-alpha")).toBeInTheDocument();
    expect(screen.getByTestId("platform-node-edge-new")).toBeInTheDocument();
    expect(screen.getByTestId("platform-reach")).toHaveTextContent(/fleet-wide read/);
    expect(screen.queryByTestId("platform-lab")).toBeNull();
    expect(screen.queryByTestId("platform-verdict")).toBeNull();
    // No pipeline without platform reach, and the reach line is said once.
    expect(screen.queryByTestId("pipeline")).toBeNull();
    expect(screen.getAllByRole("note")).toHaveLength(1);
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

  it("gives each server its own panel set, the first one open, each panel the groups and ranked routes the view returns, in order", () => {
    emptySeries();
    mount(twoServers());
    const first = screen.getByTestId("server-panels-lab-server");
    const second = screen.getByTestId("server-panels-lab-server-b");
    expect(first).toHaveAttribute("open");
    expect(second).not.toHaveAttribute("open");
    for (const set of [first, second]) {
      const names = Array.from(set.querySelectorAll("[data-panel]")).map((el) => el.getAttribute("data-panel"));
      expect(names).toEqual(["requests", "ingest", "queues", "retention"]);
    }
    const requests = within(first).getByTestId("server-panel-requests");
    const routes = within(requests).getAllByTestId("panel-row-http-request-duration-max");
    expect(routes.map((r) => r.getAttribute("data-instance"))).toEqual([SLOW, FAST]);
    expect(routes[0]).toHaveTextContent("812 ms");
    expect(within(requests).getByTestId("panel-row-db-pool-acquired")).toHaveTextContent("4 / 20");
    expect(within(first).getByTestId("server-panel-ingest")).toHaveTextContent("2.5 s");
    const lanes = within(within(first).getByTestId("server-panel-retention")).getAllByTestId("panel-row-retention-behind");
    expect(lanes.map((r) => r.getAttribute("data-instance"))).toEqual(["event", "metric"]);
  });

  it("marks each panel set's heading as a disclosure, its chevron turned while the set is open", () => {
    emptySeries();
    mount(twoServers());
    const first = screen.getByTestId("server-panels-lab-server") as HTMLDetailsElement;
    const second = screen.getByTestId("server-panels-lab-server-b") as HTMLDetailsElement;
    const marker = (set: HTMLElement) => set.querySelector(":scope > summary [data-disclosure]");
    expect(marker(first)).toHaveClass("rotate-90");
    expect(marker(second)).not.toHaveClass("rotate-90");
    second.open = true;
    fireEvent(second, new Event("toggle"));
    expect(marker(second)).toHaveClass("rotate-90");
  });

  it("renders a server with no samples yet as not reported on every panel, with no series read and no console error", () => {
    const errors = vi.spyOn(console, "error");
    const fetchSpy = emptySeries();
    mount({ ...full, servers: [{ component: uuidFor("server"), name: "lab-server", started_at: ago(1000), verdict: "unknown", groups: panels(false), alarms: [] }] });
    const set = screen.getByTestId("server-panels-lab-server");
    for (const name of ["requests", "ingest", "queues", "retention"]) {
      const panel = within(set).getByTestId(`server-panel-${name}`);
      const rows = panel.querySelectorAll("[data-testid^=panel-row-]");
      expect(rows.length).toBeGreaterThan(0);
      for (const r of Array.from(rows)) expect(r).toHaveTextContent("not reported");
    }
    // A ranked key with no window yet is one not-reported row under its own
    // name: there is no route to rank, so no instance (and no owner-level series).
    const requests = within(set).getByTestId("server-panel-requests");
    expect(requests).not.toHaveTextContent("owner-level");
    expect(requests).not.toHaveTextContent("newest window");
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(errors).not.toHaveBeenCalled();
  });

  it("flags only the panel an open alarm names a key of, with the alarm's message", () => {
    emptySeries();
    const v = twoServers();
    v.servers![0].alarms = [{ rule: "retention-behind", key: "retention-behind", severity: "warning", message: "metric retention is 1 day behind", raised_at: ago(60_000) }];
    mount(v);
    const set = screen.getByTestId("server-panels-lab-server");
    expect(within(set).getByTestId("server-panel-retention")).toHaveAttribute("data-flagged", "true");
    expect(within(set).getByTestId("server-panel-retention")).toHaveTextContent("metric retention is 1 day behind");
    expect(within(set).getByTestId("server-panel-requests")).toHaveAttribute("data-flagged", "false");
  });

  it("asks each sparkline for its own row's instance, over the last hour", async () => {
    const fetchSpy = emptySeries();
    mount(twoServers());
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const urls = fetchSpy.mock.calls.map(([req]) => decodeURIComponent(new URL(req.url).pathname + new URL(req.url).search));
    expect(urls.some((u) => u.includes(`/components/${uuidFor("server")}/metrics/http-request-duration-max/samples`) && u.includes(`instance=${FAST}`) && u.includes("hours=1"))).toBe(true);
    // Only the open set reads its series: the second server's are closed.
    expect(urls.some((u) => u.includes(uuidFor("server-b")))).toBe(false);
  });

  // The view polls, and every poll is a new body: an operator's place on the
  // page (a set they opened or closed, a row's chart, a key's tooltip) is the
  // same server, key and route in the next body, so it survives the poll.
  it("keeps an opened set, an expanded row and an open tooltip across a poll, with the poll's figures", async () => {
    emptySeries();
    const { qc } = mount(twoServers());
    const first = screen.getByTestId("server-panels-lab-server");
    const second = screen.getByTestId("server-panels-lab-server-b") as HTMLDetailsElement;
    second.open = true;
    fireEvent(second, new Event("toggle"));
    const slow = () => within(within(screen.getByTestId("server-panels-lab-server")).getByTestId("server-panel-requests")).getAllByTestId("panel-row-http-request-duration-max").find((r) => r.getAttribute("data-instance") === SLOW)!;
    const row = slow();
    fireEvent.click(row);
    expect(row).toHaveAttribute("aria-expanded", "true");
    const trigger = within(within(first).getByTestId("server-panel-ingest")).getByRole("button", { name: "Ingest Lag" });
    fireEvent.keyDown(document.body, { key: "Tab" });
    trigger.focus();
    expect(await screen.findByText("How far ingest runs behind.")).toBeInTheDocument();

    const next = twoServers();
    next.servers![0].groups![0].figures![0].value = 900;
    qc.setQueryData([...PLATFORM_VIEW_KEY], next);
    await waitFor(() => expect(slow()).toHaveTextContent("900 ms"));

    expect(slow()).toBe(row);
    expect(row).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("server-panels-lab-server-b")).toBe(second);
    expect(second.open).toBe(true);
    expect(screen.getByTestId("server-panels-lab-server")).toBe(first);
    expect(screen.getByText("How far ingest runs behind.")).toBeInTheDocument();
  });

  // The first set opens once, on mount: a server that later sorts ahead of
  // it moves the index, not the set the operator is reading, which stays open.
  it("keeps the set the operator is reading open when a new server sorts ahead of it", async () => {
    emptySeries();
    const { qc } = mount(twoServers());
    const first = screen.getByTestId("server-panels-lab-server") as HTMLDetailsElement;
    expect(first.open).toBe(true);
    const next = twoServers();
    next.servers = [
      { component: uuidFor("server-a"), name: "aa-server", version: "v9.9.9", started_at: ago(1000), verdict: "healthy", groups: panels(true), alarms: [] },
      ...next.servers!,
    ];
    qc.setQueryData([...PLATFORM_VIEW_KEY], next);
    await waitFor(() => expect(screen.getByTestId("server-panels-aa-server")).toBeInTheDocument());
    expect(screen.getByTestId("server-panels-lab-server")).toBe(first);
    expect(first.open).toBe(true);
    expect(first.querySelector(":scope > summary [data-disclosure]")).toHaveClass("rotate-90");
  });

  // A row's chart is a disclosure, so the keyboard reaches it as the pointer
  // does: a reported row takes focus and Enter or Space opens and closes it;
  // a not-reported row has no chart to open and is not a stop.
  it("opens a reported row's chart from the keyboard, and skips a row with nothing reported", () => {
    emptySeries();
    const v = twoServers();
    v.servers![0].groups![1].figures = [];
    mount(v);
    const set = screen.getByTestId("server-panels-lab-server");
    const row = within(within(set).getByTestId("server-panel-requests")).getAllByTestId("panel-row-http-request-duration-max")[0];
    expect(row).toHaveAttribute("tabindex", "0");
    fireEvent.keyDown(row, { key: "Enter" });
    expect(row).toHaveAttribute("aria-expanded", "true");
    fireEvent.keyDown(row, { key: " " });
    expect(row).toHaveAttribute("aria-expanded", "false");
    const lag = within(within(set).getByTestId("server-panel-ingest")).getByTestId("panel-row-ingest-lag");
    expect(lag).toHaveTextContent("not reported");
    expect(lag).not.toHaveAttribute("tabindex");
    fireEvent.keyDown(lag, { key: "Enter" });
    expect(lag).not.toHaveAttribute("aria-expanded");
  });

  it("teaches each key through its catalog description, with a way onto its series on the server's platform component", async () => {
    emptySeries();
    mount(twoServers());
    const requests = within(screen.getByTestId("server-panels-lab-server")).getByTestId("server-panel-requests");
    const trigger = within(requests).getByRole("button", { name: "Slowest Request" });
    fireEvent.keyDown(document.body, { key: "Tab" });
    trigger.focus();
    expect(await screen.findByText("The slowest request in the window, per route.")).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /series on lab-server/ });
    expect(link).toHaveAttribute("href", `/web/components/${uuidFor("server")}`);
    fireEvent.click(link);
    await waitFor(() => expect(window.location.pathname).toBe(`/web/components/${uuidFor("server")}`));
  });
});

// With a simulator (#1647): the section keys on the nav's "simulator"
// condition, a simulation reader with a simulator connected, never on lab.
const connected: SimFleet = { connected: true, nodes: [] };
const measured: PlatformSimNode = {
  node: "edge-alpha", state: "measured", start: ago(600_000), end: ago(60_000), brackets: 11, error: "",
  gaps: [
    { gap: "connect-not-accepted", protocol: "", count: 0, in: 40, ratio: 0, masked: 0, masked_by: "connections still in the kernel backlog at the end read", answered_late: 0, reading: "loss" },
    { gap: "sent-not-received", protocol: "http", count: 30, in: 120, ratio: 0.25, masked: 0, masked_by: "requests in flight at the node's end read", answered_late: 0, reading: "impaired" },
    { gap: "published-not-landed", protocol: "", count: 2, in: 40, ratio: 0.05, masked: 1, masked_by: "receipts landed in the margin before the window", answered_late: 0, reading: "loss" },
    { gap: "sent-not-received", protocol: "icmp", count: 0, in: 0, ratio: null, masked: 0, masked_by: "", answered_late: 0, reading: "not observed by the sim" },
  ],
  receipts: { landed: 42, margin: 1, backlog: 0, after_end: 0 },
  protocols: [{ protocol: "http", accepted: 9, received: 90, answered: 90, latency_ms: { p50: 1, p95: 2.5, p99: 4, n: 90 }, poll_targets: 1, in_flight_high: 2 }],
  in_flight_high: 2, queue_depth: null, busy_workers: null,
  devices: [{ component_id: uuidFor("display"), component: "lobby-display", received: { http: 90 }, impaired: true, unhit: false }],
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
    expect(rows[2]).toHaveTextContent("any listener");
    expect(rows[2].className).toBe("");
  });

  it("links each device to its drawer on the Simulation page and names the figures not wired yet", () => {
    mount(full, { fleet: connected, body: simBody([measured]) });
    const device = screen.getByTestId("platform-sim-device");
    expect(within(device).getByRole("link", { name: "lobby-display" })).toHaveAttribute("href", `/web/simulation?c=${uuidFor("display")}`);
    expect(within(device).getByText("impaired")).toBeInTheDocument();
    expect(screen.getByTestId("platform-sim-protocol")).toHaveAttribute("data-accepted", "9");
    expect(screen.getByTestId("platform-sim-protocol")).toHaveAttribute("data-received", "90");
    expect(screen.getByTestId("platform-sim-protocol")).toHaveAttribute("data-poll-targets", "1");
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
