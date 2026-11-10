import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { fireEvent, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import PlatformHealth from "./PlatformHealth";
import { PLATFORM_VIEW_KEY, type PlatformView } from "../lib/platformhealth";
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

// A seeded view mounts on a client that never retries; a refused one (null)
// mounts on the production default, three retries with backoff, so only the
// page's own retry policy can surface the refusal promptly.
function mount(view: PlatformView | null) {
  const qc = new QueryClient({ defaultOptions: { queries: view ? { staleTime: Infinity, retry: false } : { staleTime: Infinity } } });
  if (view) qc.setQueryData([...PLATFORM_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], viewer);
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
    expect(within(alpha).getByRole("link", { name: "Edge Alpha" })).toHaveAttribute("href", `/components/${uuidFor("alpha")}`);
    expect(within(alpha).getByRole("link", { name: "Open edge-alpha in Nodes" })).toHaveAttribute("href", "/nodes?n=edge-alpha");
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
});
