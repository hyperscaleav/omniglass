import { describe, it, expect, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Components from "./Components";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { componentSystemsKey } from "../lib/members";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
import { componentAlarmsKey } from "../lib/alarms";
import { effectivePropertiesKey } from "../lib/component_properties";
import { effectiveMetricsKey } from "../lib/component_metrics";
import { NODES_KEY, type Node } from "../lib/nodes";
import { PRODUCTS_KEY } from "../lib/products";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { TAGS_KEY } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The component leaf (#637): direct arrival renders the breadcrumb from the
// ancestor chain, memberships list with the primary marked, and the
// collection card states the distinction plainly: a stale sample under a
// healthy node blames the device or the path, never collection.

const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

const view: FleetView = {
  locations: [
    { id: uuidFor("cf-hq"), name: "hq", label: "Headquarters", location_type: "campus", location_type_id: uuidFor("cft-campus"), parent: "", verdict: "healthy" },
    { id: uuidFor("cf-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", location_type_id: uuidFor("cft-room"), parent: uuidFor("cf-hq"), verdict: "healthy" },
  ],
  systems: [
    { id: uuidFor("cf-s-a"), name: "boardroom", label: "Boardroom System", location: uuidFor("cf-room"), verdict: "degraded", dots: [{ component: uuidFor("cf-c-bar"), name: "videobar-1", verdict: "outage", primary: true, shared: false }] },
    { id: uuidFor("cf-s-b"), name: "overflow", label: "Overflow", location: null, verdict: "healthy", dots: [] },
  ],
} as unknown as FleetView;

const bar: Component = {
  id: uuidFor("cf-c-bar"),
  name: "videobar-1",
  label: "Video Bar 1",
  location: "boardroom-a",
  location_id: uuidFor("cf-room"),
  product: "kestrel-vroom",
} as unknown as Component;

const now = Date.now();
const iso = (secondsAgo: number) => new Date(now - secondsAgo * 1000).toISOString();

const reach: Reachability = {
  component: "videobar-1",
  endpoints: [
    // A stale sample: last verdict long ago, but the node below is alive.
    { endpoint: "ssh", transport: "ssh", node: "edge-1", verdict: { value: "up", ts: iso(600) }, layers: [
      { layer: "ping", check: "icmp-reachable", value: 1, ts: iso(600) },
      { layer: "port", check: "tcp-open", value: 1, ts: iso(600) },
    ], history: [] },
  ],
} as unknown as Reachability;

const nodes: Node[] = [{ name: "edge-1", enrolled: true, last_heartbeat_at: iso(5), tags: {} } as Node];

function mount(path = `/web/components/${uuidFor("cf-c-bar")}`, memberships?: unknown[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...COMPONENTS_KEY], [bar]);
  qc.setQueryData([...PRODUCTS_KEY], [{ id: uuidFor("cf-p"), name: "kestrel-vroom", label: "Kestrel VRoom", vendor: "kestrel", driver: "kestrel-http" }]);
  qc.setQueryData([...NODES_KEY], nodes);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...componentSystemsKey(uuidFor("cf-c-bar"))], memberships ?? [
    { component: "videobar-1", system: "boardroom", primary: true, system_count: 2 },
    { component: "videobar-1", system: "overflow", primary: false, system_count: 2 },
  ]);
  qc.setQueryData([...REACHABILITY_KEY(uuidFor("cf-c-bar"))], reach);
  qc.setQueryData([...componentAlarmsKey(uuidFor("cf-c-bar"))], [
    { id: "al-1", component: "videobar-1", severity: "critical", message: "No route to host", raised_at: iso(9600), active: true, acknowledged: false },
    { id: "al-0", component: "videobar-1", severity: "warning", message: "Cleared last week", raised_at: iso(600000), active: false, acknowledged: true },
  ]);
  qc.setQueryData([...effectivePropertiesKey(uuidFor("cf-c-bar"))], [
    { property_type_name: "serial-number", label: "Serial Number", value: "SN-1183", is_set: true, from_contract: true },
    { property_type_name: "firmware-version", value: "2.1.4", is_set: false, from_contract: true },
    { property_type_name: "mac-address", value: null, is_set: false, from_contract: true },
  ]);
  qc.setQueryData([...effectiveMetricsKey(uuidFor("cf-c-bar"))], [
    { metric_type_name: "icmp-rtt-avg", label: "RTT avg", data_type: "float", value: 6.1, is_sampled: true, from_contract: false, required: false },
  ]);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/components/:id" component={Components} />
        <Route path="/fleet" component={() => <div data-testid="fleet-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("the component leaf", () => {
  it("arriving directly renders the breadcrumb from the ancestor chain through the primary system, no prior navigation", () => {
    mount();
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getByText("Fleet")).toBeTruthy();
    expect(within(trail).getByText("Headquarters")).toBeTruthy();
    expect(within(trail).getByText("Boardroom A")).toBeTruthy();
    // The primary system is the last crumb; the leaf itself is the title.
    expect(within(trail).getByText("Boardroom System")).toBeTruthy();
    expect(within(trail).queryByText("Video Bar 1")).toBeNull();
    expect(screen.getByRole("heading", { name: "Video Bar 1" })).toBeTruthy();
  });

  it("says what it is: product (label with its handle), vendor, driver, once each", () => {
    mount();
    const card = screen.getByTestId("leaf-identity");
    expect(within(card).getByText("kestrel-vroom")).toBeTruthy();
    expect(within(card).getByText("kestrel")).toBeTruthy();
    expect(within(card).getByText("kestrel-http")).toBeTruthy();
    // No slug sentence restating the rows above it.
    expect(within(card).queryByText(/driven by/)).toBeNull();
  });

  it("says where it sits: the clickable chain (each crumb's type as its tooltip) and the primary system", () => {
    mount();
    const card = screen.getByTestId("leaf-placement");
    const room = within(card).getByRole("button", { name: "Boardroom A" });
    expect(room.getAttribute("title")).toBe("room");
    expect(within(card).queryByTestId("leaf-type-path")).toBeNull();
    expect(within(card).getByRole("button", { name: "Boardroom System" })).toBeTruthy();
  });

  it("wears the same shell as every zoom: the fleet-wide summary rail on top, no right rail", () => {
    mount();
    expect(screen.getByTestId("fleet-summary")).toBeTruthy();
    expect(screen.queryByTestId("zoom-rail")).toBeNull();
  });

  it("lists one row per membership with the primary marked, and says the location comes from the primary", () => {
    mount();
    const card = screen.getByTestId("leaf-memberships");
    expect(within(card).getByText("Boardroom System")).toBeTruthy();
    expect(within(card).getByText("Overflow")).toBeTruthy();
    expect(within(card).getByText("primary")).toBeTruthy();
    expect(within(card).getByText(/Location follows the primary system/)).toBeTruthy();
  });

  it("distinguishes a stale sample under a healthy node from an offline node", () => {
    mount();
    const chip = screen.getByTestId("collection-ssh");
    expect(chip.textContent).toContain("check the device or the path");
  });

  it("a component filling no role renders without error", () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}`, []);
    expect(screen.getByText(/Not in any system yet/)).toBeTruthy();
    expect(screen.getByTestId("leaf-collection")).toBeTruthy();
  });
});

describe("the leaf's dispatch facts (#786)", () => {
  it("the header wears the component's verdict and since-when from the worst active alarm", () => {
    mount();
    const header = screen.getByTestId("leaf-header");
    expect(within(header).getByText("outage")).toBeTruthy();
    expect(within(header).getByText(/since/)).toBeTruthy();
  });

  it("renders the active alarms with severity, message and ack state, and never the cleared history", () => {
    mount();
    const card = screen.getByTestId("leaf-alarms");
    expect(within(card).getByText("No route to host")).toBeTruthy();
    expect(within(card).getByText(/unacknowledged/)).toBeTruthy();
    expect(within(card).queryByText("Cleared last week")).toBeNull();
  });

  it("the identity panel carries the resolved properties: serial and firmware beside product, vendor, driver", () => {
    mount();
    const card = screen.getByTestId("leaf-identity");
    expect(within(card).getByText("SN-1183")).toBeTruthy();
    expect(within(card).getByText("2.1.4")).toBeTruthy();
    // A property with no value is not a row.
    expect(within(card).queryByText(/mac-address/)).toBeNull();
  });

  it("vitals list the effective metrics that carry a value, marking the live series", () => {
    mount();
    const card = screen.getByTestId("leaf-vitals");
    expect(within(card).getByText("RTT avg")).toBeTruthy();
    expect(within(card).getByText("6.1")).toBeTruthy();
  });

  it("the collection card shows each interface's layer rungs", () => {
    mount();
    const card = screen.getByTestId("leaf-collection");
    expect(within(card).getByText("ping up")).toBeTruthy();
    expect(within(card).getByText("port open")).toBeTruthy();
  });
});


// The leaf grows the Configure facet too (#800 slice 1): identity with the
// reset-to-generated affordance, the product read-only (fixed at creation),
// tags. Placement reads where it sits; movers are not part of the classic
// contract this slice reproduces.
describe("the component configure tab (#800)", () => {
  it("offers Configure with identity, the fixed product, and tags", async () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    expect(within(face).getByText("Identity")).toBeTruthy();
    expect(within(face).getByText("Classification")).toBeTruthy();
    expect(within(face).getAllByText("Tags").length).toBeGreaterThan(0);
    expect(within(face).getByText(/fixed at creation/i)).toBeTruthy();
  });
});


// The interfaces surface the classic face carried, re-homed on the leaf's
// Configure (#800 slice 3): the reachability panel with its add and open
// affordances wiring the interface blades onto the leaf's own stack.
describe("interfaces live on the leaf configure (#800)", () => {
  it("offers Add interface and opens the create blade on this stack", async () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    const add = await within(face).findByRole("button", { name: /add interface/i });
    fireEvent.click(add);
    const blade = await screen.findByRole("dialog");
    expect(blade.getAttribute("aria-labelledby")).toContain("endpoint-create");
  });
});

describe("the miss face (#800)", () => {
  it("an address matching no component renders the explicit miss, not a silent fallback", async () => {
    mount("/web/components/no-such-widget");
    expect(await screen.findByText(/No component answers this address/)).toBeTruthy();
    expect(screen.queryByText("Reachability")).toBeNull();
  });
});
