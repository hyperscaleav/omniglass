import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Components from "./Components";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { componentSystemsKey } from "../lib/members";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
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
    { id: uuidFor("cf-s-a"), name: "boardroom", label: "Boardroom System", location: uuidFor("cf-room"), verdict: "healthy", dots: [] },
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
  interfaces: [
    // A stale sample: last verdict long ago, but the node below is alive.
    { interface: "ssh", interface_type: "ssh", node: "edge-1", verdict: { value: "up", ts: iso(600) }, layers: [], history: [] },
  ],
} as unknown as Reachability;

const nodes: Node[] = [{ name: "edge-1", enrolled: true, last_heartbeat_at: iso(5), tags: {} } as Node];

function mount(path = `/web/components/${uuidFor("cf-c-bar")}?zoom=1`, memberships?: unknown[]) {
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
    mount(`/web/components/${uuidFor("cf-c-bar")}?zoom=1`, []);
    expect(screen.getByText(/Not in any system yet/)).toBeTruthy();
    expect(screen.getByTestId("leaf-collection")).toBeTruthy();
  });
});
