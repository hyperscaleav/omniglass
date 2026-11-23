import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import DependenciesPanel from "./DependenciesPanel";
import { dependenciesKey, type Dependencies, type Dependency } from "../lib/dependencies";
import { COMPONENTS_KEY, type Component as Comp } from "../lib/components";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { REACHABILITY_KEY } from "../lib/reachability";
import { uuidFor } from "../lib/testids";

// The Dependencies panel on a component's page (#2428): the trio is the
// seeded one, a bar with one upstream (the power conditioner) and two
// downstreams (the touch panel and the display). Reads are seeded into the
// query cache; writes are faked at fetch and the refetch answers with the
// server's state after, so a test sees the list move as an operator does.
const BAR = uuidFor("bar-a");
const PDU = uuidFor("power-1");
const PANEL = uuidFor("panel-1");
const DISPLAY = uuidFor("display-1");
const SWITCH = uuidFor("switch-1");
const now = Date.now();

const edge = (component: string, upstream: string, kind: Dependency["kind"], outlet?: string): Dependency => ({
  component,
  component_id: uuidFor(component),
  upstream,
  upstream_id: uuidFor(upstream),
  kind,
  outlet,
  created_at: new Date(now).toISOString(),
});

const trio: Dependencies = {
  component: "bar-a",
  upstreams: [edge("bar-a", "power-1", "power", "1")],
  dependents: [edge("panel-1", "bar-a", "host"), edge("display-1", "bar-a", "path"), edge("panel-1", "bar-a", "path")],
  hidden: 0,
};

const comps: Comp[] = [
  { id: BAR, name: "bar-a", label: "Video bar A", system_count: 1, actions: ["update"] },
  { id: PDU, name: "power-1", label: "Power conditioner", system_count: 0, actions: ["update"] },
  { id: PANEL, name: "panel-1", label: "Touch panel", system_count: 1, actions: ["update"] },
  { id: DISPLAY, name: "display-1", label: "Room display", system_count: 1, actions: ["update"] },
  { id: SWITCH, name: "switch-1", label: "Core switch", system_count: 0, actions: ["update"] },
];

const fleet = {
  locations: [],
  systems: [
    {
      id: uuidFor("sys"),
      name: "boardroom-a",
      health: "degraded",
      dots: [
        { component: BAR, health: "healthy" },
        { component: PANEL, health: "degraded" },
        { component: DISPLAY, health: "outage" },
      ],
    },
  ],
} as unknown as FleetView;

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(opts: { deps?: Dependencies; canUpdate?: boolean; holdBy?: string } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...dependenciesKey(BAR)], opts.deps ?? trio);
  qc.setQueryData([...COMPONENTS_KEY], comps);
  qc.setQueryData([...FLEET_VIEW_KEY], fleet);
  qc.setQueryData([...REACHABILITY_KEY(BAR)], {
    component: "bar-a",
    endpoints: opts.holdBy ? [{ endpoint: "tcp", gate_reasons: [{ kind: "upstream", source: opts.holdBy, since: new Date(now).toISOString(), component: "power-1" }] }] : [],
  });
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <DependenciesPanel component={BAR} canUpdate={opts.canUpdate ?? true} />} />
      </Router>
    </QueryClientProvider>
  ));
  return { ...result, qc };
}

describe("DependenciesPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the upstream and both downstreams, each linked and with its health", () => {
    const q = mount();
    const up = within(q.getByTestId("dep-upstream"));
    const link = up.getByRole("link", { name: "Power conditioner" });
    expect(link.getAttribute("href")).toBe(`/components/${PDU}`);
    expect(up.getByText(/outlet 1/).textContent).toContain("Power");
    const down = within(q.getByTestId("dep-downstream"));
    expect(down.getByRole("link", { name: "Touch panel" }).getAttribute("href")).toBe(`/components/${PANEL}`);
    expect(down.getByRole("link", { name: "Room display" })).toBeTruthy();
    expect(down.getByText("degraded")).toBeTruthy();
    expect(down.getByText("outage")).toBeTruthy();
    // The panel is hosted by the bar and reached through it: one row, two kinds.
    expect(down.getAllByRole("link").length).toBe(2);
    expect(within(down.getByTestId(`dep-down-${PANEL}`)).getAllByText(/^(Host|Path)$/).length).toBe(2);
    // The power conditioner sits in no system, so it has no health to show.
    expect(up.getByText("unknown")).toBeTruthy();
  });

  it("marks the upstream that is holding this component right now", () => {
    const q = mount({ holdBy: PDU });
    expect(within(q.getByTestId("dep-upstream")).getByTestId("dep-holding")).toBeTruthy();
    expect(within(q.getByTestId("dep-downstream")).queryByTestId("dep-holding")).toBeNull();
  });

  it("shows no hold when nothing gates it", () => {
    const q = mount();
    expect(q.queryByTestId("dep-holding")).toBeNull();
  });

  it("counts edges the caller cannot read, never naming them", () => {
    const q = mount({ deps: { ...trio, hidden: 3 } });
    expect(within(q.getByTestId("dep-downstream")).getByText("and 3 more you cannot see")).toBeTruthy();
  });

  it("is read-only for a viewer: no add, no remove", () => {
    const q = mount({ canUpdate: false });
    expect(q.queryByRole("button", { name: "Add upstream" })).toBeNull();
    expect(q.queryByRole("button", { name: /^Remove / })).toBeNull();
    expect(q.getByText("Power conditioner")).toBeTruthy();
  });

  it("renders nothing for a viewer when the component has no edges", () => {
    const q = mount({ canUpdate: false, deps: { component: "bar-a", upstreams: [], dependents: [], hidden: 0 } });
    expect(q.queryByTestId("leaf-dependencies")).toBeNull();
  });

  it("says so in one line when it has no edges, and still offers Add", () => {
    const q = mount({ deps: { component: "bar-a", upstreams: [], dependents: [], hidden: 0 } });
    expect(q.getByText("No dependencies recorded.")).toBeTruthy();
    expect(q.getByRole("button", { name: "Add upstream" })).toBeTruthy();
  });

  it("filters the picker and never offers the component itself", () => {
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Add upstream" }));
    const picker = q.getByLabelText("Upstream component") as HTMLSelectElement;
    const names = () => Array.from(picker.options).map((o) => o.textContent);
    expect(names()).not.toContain("Video bar A");
    expect(names()).toContain("Core switch");
    fireEvent.input(q.getByLabelText("Filter components"), { target: { value: "switch" } });
    expect(names().filter((n) => n && !n.startsWith("Pick"))).toEqual(["Core switch"]);
  });

  it("adds a dependency, sends the kind and outlet, and the list gains the row", async () => {
    let post: { url: string; body: unknown } | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        post = { url: req.url, body: await req.json() };
        return json(edge("bar-a", "switch-1", "path"), 201);
      }
      if (req.url.includes("/dependencies")) return json({ ...trio, upstreams: [...trio.upstreams!, edge("bar-a", "switch-1", "path")] });
      return json({});
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Add upstream" }));
    fireEvent.change(q.getByLabelText("Upstream component"), { target: { value: SWITCH } });
    fireEvent.change(q.getByLabelText("Dependency kind"), { target: { value: "path" } });
    // A path edge has no outlet.
    expect(q.queryByLabelText("Outlet")).toBeNull();
    fireEvent.click(q.getByRole("button", { name: "Add dependency" }));
    await waitFor(() => expect(post).toBeTruthy());
    expect(post!.url).toContain(`/components/${BAR}/dependencies`);
    expect(post!.body).toEqual({ upstream: SWITCH, kind: "path" });
    await waitFor(() => expect(within(q.getByTestId("dep-upstream")).getByRole("link", { name: "Core switch" })).toBeTruthy());
    expect(q.queryByTestId("dep-form")).toBeNull();
  });

  it("holds Add until a power edge names its outlet", () => {
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Add upstream" }));
    fireEvent.change(q.getByLabelText("Upstream component"), { target: { value: SWITCH } });
    const add = q.getByRole("button", { name: "Add dependency" }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    fireEvent.input(q.getByLabelText("Outlet"), { target: { value: "4" } });
    expect(add.disabled).toBe(false);
  });

  it("shows the server's refusal of a cycle in place and keeps the form open", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        return json({ status: 422, title: "Unprocessable Entity", detail: "the upstream already depends on this component through edges of this kind, so this edge would close a cycle" }, 422);
      }
      return json(trio);
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Add upstream" }));
    fireEvent.change(q.getByLabelText("Upstream component"), { target: { value: PANEL } });
    fireEvent.change(q.getByLabelText("Dependency kind"), { target: { value: "host" } });
    fireEvent.click(q.getByRole("button", { name: "Add dependency" }));
    const alert = await q.findByRole("alert");
    expect(alert.textContent).toContain("would close a cycle");
    expect(q.getByTestId("dep-form")).toBeTruthy();
  });

  it("removes an upstream after a confirm, and the list loses the row", async () => {
    let del: string | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") {
        del = req.url;
        return new Response(null, { status: 204 });
      }
      return json({ ...trio, upstreams: [] });
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Remove Power conditioner as power upstream" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText(/stops being a power upstream/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(del).toBeTruthy());
    expect(del).toContain(`/components/${BAR}/dependencies/power/${PDU}`);
    await waitFor(() => expect(q.getByText("Depends on nothing.")).toBeTruthy());
  });

  it("removes nothing when the confirm is cancelled", async () => {
    const spy = vi.spyOn(globalThis, "fetch");
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Remove Power conditioner as power upstream" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(spy).not.toHaveBeenCalled();
    expect(q.getByText("Power conditioner")).toBeTruthy();
  });

  it("shows a refused remove in the confirm and keeps the row", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") return json({ status: 403, title: "Forbidden", detail: "outside your component:update scope" }, 403);
      return json(trio);
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Remove Power conditioner as power upstream" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(within(dialog).getByRole("alert").textContent).toContain("Not allowed: outside your component:update scope"));
    expect(q.getByTestId("dep-upstream")).toBeTruthy();
  });

  it("offers no remove on a downstream row: that edge is the dependent's own", () => {
    const q = mount();
    expect(within(q.getByTestId("dep-downstream")).queryByRole("button")).toBeNull();
  });
});
