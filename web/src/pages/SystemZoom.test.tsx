import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within, cleanup, fireEvent } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
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
        { component: uuidFor("szp-c-mic"), name: "mic-1", verdict: "outage", primary: true, shared: false },
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
      name: "room-mic", label: "Room Microphone", impact: "degraded", quorum: 2, satisfying: 1, short: 1, spare: 1,
      impaired: true, active: true, assigned_to: ["videobar-1", "mic-1"], down: ["mic-1"],
      alarms: [{ id: "al-1", component: "mic-1", severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" }],
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
  transitions: [
    { ts: "2026-08-01T09:00:00Z", verdict: "healthy" },
    { ts: "2026-08-15T14:20:00Z", verdict: "degraded" },
  ],
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

function mount(path = `/web/systems/${uuidFor("szp-sys")}?zoom=1`, healthOverride: FleetHealth = health) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemHealthKey(uuidFor("szp-sys"))], healthOverride);
  qc.setQueryData([...systemRolesKey(uuidFor("szp-sys"))], declared);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/systems/:id" component={Systems} />
        <Route path="/locations/:id" component={() => <div data-testid="location-page" />} />
        <Route path="/fleet" component={() => <div data-testid="fleet-page" />} />
        <Route
          path="/components/:id"
          component={() => {
            const loc = useLocation();
            return <div data-testid="component-page">{loc.pathname + loc.search}</div>;
          }}
        />
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

  // The drilldown's last step: an occupant is the component, so clicking it
  // opens the leaf by uuid (names repeat across rooms) and keeps the zoom.
  it("clicking an occupant opens the component leaf, by id, keeping the zoom", async () => {
    mount();
    const bar = screen.getByTestId("slot-conf-bar");
    fireEvent.click(within(bar).getByRole("button", { name: /videobar-1/ }));
    const page = await screen.findByTestId("component-page");
    expect(page.textContent).toBe(`/web/components/${uuidFor("szp-c-bar")}?zoom=1`);
  });

  it("clicking a no-role member opens its leaf too", async () => {
    mount();
    const strip = screen.getByTestId("no-role-strip");
    fireEvent.click(within(strip).getByRole("button", { name: /device-1/ }));
    const page = await screen.findByTestId("component-page");
    expect(page.textContent).toBe(`/web/components/${uuidFor("szp-c-power")}?zoom=1`);
  });

  // #785: the header answers since-when, the alarms lead, the history strip
  // renders, and slot arithmetic stops leading a fully staffed room.
  it("the header names the last edge and its age; slot arithmetic leads only while something is missing", () => {
    mount();
    const header = screen.getByTestId("system-header");
    expect(within(header).getByText(/since/)).toBeTruthy();
    // This fixture IS short, so the line earns its place.
    expect(within(header).getByText(/slots filled/)).toBeTruthy();
  });

  it("a fully staffed room says nothing about slots: deployed is the norm, not an achievement", () => {
    const full = {
      ...health,
      verdict: "healthy",
      roles: (health.roles ?? []).map((r) => ({
        ...r,
        satisfying: r.quorum,
        short: 0,
        impaired: false,
        down: [],
        alarms: [],
        assigned_to: Array.from({ length: Math.max(r.quorum, (r.assigned_to ?? []).length) }, (_, i) => (r.assigned_to ?? [])[i] ?? `filler-${r.name}-${i}`),
      })),
    } as FleetHealth;
    mount(undefined, full);
    const header = screen.getByTestId("system-header");
    expect(within(header).queryByText(/slots filled/)).toBeNull();
  });

  it("renders the active alarms worst first, above the roles, each naming its component and role", () => {
    mount();
    const strip = screen.getByTestId("alarm-strip");
    expect(within(strip).getByText("No route to host")).toBeTruthy();
    expect(within(strip).getByText(/Room Microphone/)).toBeTruthy();
    expect(within(strip).getByRole("button", { name: /mic-1/ })).toBeTruthy();
  });

  it("renders the verdict history strip from the transitions", () => {
    mount();
    expect(screen.getByTestId("health-history")).toBeTruthy();
  });

  it("a role holding more occupants than its quorum says so", () => {
    mount();
    const card = screen.getByTestId("slot-room-mic");
    expect(within(card).getByText(/\+ 1 spare/)).toBeTruthy();
  });

  it("without the zoom param the route renders the inventory detail, untouched", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}`);
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });
});
