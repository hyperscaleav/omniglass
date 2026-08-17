import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Systems from "./Systems";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { systemRolesKey } from "../lib/system_roles";
import { SYSTEMS_KEY } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY } from "../lib/tags";
import { uuidFor } from "../lib/testids";
import type { EffectiveRole } from "../lib/system_zoom";

// The system zoom's chrome (#636): one card per role with the server's own
// arithmetic, choices grouped with the active alternate marked and the losing
// build quiet, shared occupants chipped, and the no-role strip a state rather
// than an error. Rendered at the identity route behind ?zoom=1 (ADR-0126).

const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

const view: FleetView = {
  locations: [
    {
      id: uuidFor("szp-room"),
      name: "boardroom-a",
      label: "Boardroom A",
      location_type: "room",
      location_type_id: uuidFor("szpt-room"),
      parent: "",
      verdict: "degraded",
    },
  ],
  systems: [
    {
      id: uuidFor("szp-sys"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("szp-room"),
      verdict: "degraded",
      dots: [
        { component: uuidFor("szp-c-bar"), name: "videobar-1", verdict: "healthy", primary: true, shared: true },
        { component: uuidFor("szp-c-power"), name: "device-1", verdict: "healthy", primary: true, shared: false },
      ],
    },
    {
      id: uuidFor("szp-other"),
      name: "overflow",
      label: "Overflow Room",
      location: null,
      verdict: "healthy",
      dots: [{ component: uuidFor("szp-c-bar"), name: "videobar-1", verdict: "healthy", primary: false, shared: true }],
    },
  ],
} as unknown as FleetView;

const health: FleetHealth = {
  verdict: "incomplete",
  roles: [
    {
      name: "main-display", label: "Main Display", impact: "outage", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: true, assigned_to: [], down: [], alarms: [],
    },
    {
      name: "conf-bar", label: "Conferencing Bar", impact: "outage", quorum: 1, satisfying: 1, short: 0, spare: 0,
      impaired: false, active: true, assigned_to: ["videobar-1"], down: [], alarms: [],
      choice: "conferencing", alternate: "all-in-one",
    },
    {
      name: "conf-codec", label: "conf-codec", impact: "outage", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: false, assigned_to: [], down: [], alarms: [],
      choice: "conferencing", alternate: "component-system",
    },
  ],
  systems: [],
  transitions: [],
} as unknown as FleetHealth;

const declared = [
  {
    name: "main-display", label: "Main Display", quorum: 1, assigned: 0, impact: "outage", from_standard: true,
    accepted_types: ["display"], pinned_products: [], assigned_to: [], positions: [], position_labels: [],
  },
  {
    name: "conf-bar", label: "Conferencing Bar", quorum: 1, assigned: 1, impact: "outage", from_standard: true,
    accepted_types: ["video-bar"], pinned_products: [], assigned_to: ["videobar-1"], alternate: "conferencing/all-in-one",
    positions: [1], position_labels: [],
  },
  {
    name: "conf-codec", label: "conf-codec", quorum: 1, assigned: 0, impact: "outage", from_standard: true,
    accepted_types: ["codec"], pinned_products: [], assigned_to: [], alternate: "conferencing/component-system",
    positions: [], position_labels: [],
  },
] as unknown as EffectiveRole[];

function mount(path = `/web/systems/${uuidFor("szp-sys")}?zoom=1`) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemHealthKey(uuidFor("szp-sys"))], health);
  qc.setQueryData([...systemRolesKey(uuidFor("szp-sys"))], declared);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/systems/:id" component={Systems} />
        <Route path="/locations/:id" component={() => <div data-testid="location-page" />} />
        <Route path="/fleet" component={() => <div data-testid="fleet-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("the system zoom", () => {
  it("renders one card per role with the server's own arithmetic", () => {
    mount();
    const display = screen.getByTestId("slot-main-display");
    expect(within(display).getByText("0 of 1 satisfying")).toBeTruthy();
  });

  it("a role nobody staffed reads incomplete, visually distinct from a role whose occupant is down", () => {
    mount();
    const display = screen.getByTestId("slot-main-display");
    expect(within(display).getByText("incomplete")).toBeTruthy();
    expect(within(display).queryByText("outage")).toBeNull();
  });

  it("shows only the build in use for a choice, named, and never the alternate the room did not choose", () => {
    mount();
    const conf = screen.getByTestId("choice-conferencing");
    expect(within(conf).getByText(/built as all-in-one/)).toBeTruthy();
    expect(within(conf).getByTestId("slot-conf-bar")).toBeTruthy();
    // The losing build's role is not on the page at all: it is a
    // configuration fact for the standard editor, not an operational one.
    expect(screen.queryByTestId("slot-conf-codec")).toBeNull();
    expect(screen.queryByText(/not the build in use/)).toBeNull();
  });

  it("an occupant serving another system carries a chip naming it", () => {
    mount();
    const bar = screen.getByTestId("slot-conf-bar");
    expect(within(bar).getByText(/also Overflow Room/)).toBeTruthy();
  });

  it("a member filling no role appears in the no-role strip, and is not an error", () => {
    mount();
    const strip = screen.getByTestId("no-role-strip");
    expect(within(strip).getByText("device-1")).toBeTruthy();
  });

  it("without the zoom param the route renders the inventory detail, untouched", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}`);
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });
});
