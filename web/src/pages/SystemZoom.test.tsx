import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within, cleanup, fireEvent } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Systems from "./Systems";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { systemRolesKey } from "../lib/system_roles";
import { SYSTEMS_KEY } from "../lib/systems";
import { systemMetricsKey } from "../lib/system_metrics";
import { STANDARDS_KEY } from "../lib/standards";
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
      alarms: [{ id: "al-1", component: uuidFor("szp-c-mic"), severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" }],
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
    name: "room-mic", label: "Room Microphone", quorum: 2, assigned: 2, impact: "degraded", from_standard: true,
    accepted_types: ["video-bar"], pinned_products: [], assigned_to: ["videobar-1", "mic-1"], positions: [1, 2], position_labels: [],
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

function mount(path = `/web/systems/${uuidFor("szp-sys")}?zoom=1`, healthOverride: FleetHealth = health, metrics: unknown[] = [], standards: unknown[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], [{ id: uuidFor("szp-sys"), name: "boardroom", label: "Boardroom", standard: "huddle-room" }]);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemHealthKey(uuidFor("szp-sys"))], healthOverride);
  qc.setQueryData([...systemRolesKey(uuidFor("szp-sys"))], declared);
  qc.setQueryData([...systemMetricsKey(uuidFor("szp-sys"))], metrics);
  qc.setQueryData([...STANDARDS_KEY], standards);
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
  // #790 reshaped the body to components-first; the behaviors the retired
  // one-card-per-role tests pinned live on in the group and card forms below.
  it("arithmetic renders only where a role earned a group; the 1:1 case carries none", () => {
    mount();
    expect(within(screen.getByTestId("rolegroup-room-mic")).getByText(/1 of 2/)).toBeTruthy();
    expect(within(screen.getByTestId(`compcard-${uuidFor("szp-c-bar")}`)).queryByText(/of \d/)).toBeNull();
  });

  it("an unstaffed role and a down occupant stay visually distinct: commissioning wears incomplete, failure wears the impact", () => {
    mount();
    expect(screen.getByTestId("rolegroup-main-display").className).toContain("border-incomplete");
    expect(screen.getByTestId("rolegroup-room-mic").className).toContain("border-warning");
    expect(within(screen.getByTestId(`compcard-${uuidFor("szp-c-mic")}`)).getByText("mic-1")).toBeTruthy();
  });

  it("the build not in use never renders", () => {
    mount();
    expect(screen.queryByText(/conf-codec/)).toBeNull();
  });

  it("an occupant serving another system carries a badge naming it", () => {
    mount();
    const card = screen.getByTestId(`compcard-${uuidFor("szp-c-bar")}`);
    expect(within(card).getByText(/also Overflow Room/)).toBeTruthy();
  });

  it("a no-role member's card opens its leaf too", async () => {
    mount();
    fireEvent.click(screen.getByTestId(`compcard-${uuidFor("szp-c-power")}`));
    const page = await screen.findByTestId("component-page");
    expect(page.textContent).toBe(`/web/components/${uuidFor("szp-c-power")}?zoom=1`);
  });

  it("a spare beyond quorum reads on the group header", () => {
    mount();
    expect(within(screen.getByTestId("rolegroup-room-mic")).getByText(/\+ 1 spare/)).toBeTruthy();
  });


  // #785: the header answers since-when, the alarms lead, the history strip renders.
  it("the header names the last edge and its age; slot arithmetic leads only while something is missing", () => {
    mount();
    const header = screen.getByTestId("system-header");
    expect(within(header).getByText(/since/)).toBeTruthy();
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
    expect(within(screen.getByTestId("system-header")).queryByText(/slots filled/)).toBeNull();
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

  it("without the zoom param the route renders the inventory detail, untouched", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}`);
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });
});

describe("the components-first body (#790)", () => {
  it("a 1:1 occupant is one card with a role badge, no box and no arithmetic", () => {
    mount();
    const card = screen.getByTestId(`compcard-${uuidFor("szp-c-bar")}`);
    expect(within(card).getByText("videobar-1")).toBeTruthy();
    expect(within(card).getByText("Conferencing Bar")).toBeTruthy();
    expect(within(card).queryByText(/satisfying/)).toBeNull();
  });

  it("the choice jargon never renders: no 'built as', no choice eyebrow", () => {
    mount();
    expect(screen.queryByText(/built as/)).toBeNull();
    expect(screen.queryByText(/conferencing$/i)).toBeNull();
  });

  it("an unstaffed role renders as an empty group wearing its badge and arithmetic", () => {
    mount();
    const g = screen.getByTestId("rolegroup-main-display");
    expect(within(g).getByText("Main Display")).toBeTruthy();
    expect(within(g).getByText(/0 of 1/)).toBeTruthy();
  });

  it("a short role renders as a group with its occupants inside and the gap named", () => {
    mount();
    const g = screen.getByTestId("rolegroup-room-mic");
    expect(within(g).getByText(/1 of 2/)).toBeTruthy();
    expect(within(g).getByTestId(`compcard-${uuidFor("szp-c-mic")}`)).toBeTruthy();
  });

  it("a no-role member is a card with the no-role badge, not a strip of chips", () => {
    mount();
    const card = screen.getByTestId(`compcard-${uuidFor("szp-c-power")}`);
    expect(within(card).getByText("no role")).toBeTruthy();
    expect(screen.queryByTestId("no-role-strip")).toBeNull();
  });

  it("clicking a card opens the component leaf, keeping the zoom", async () => {
    mount();
    fireEvent.click(screen.getByTestId(`compcard-${uuidFor("szp-c-bar")}`));
    const page = await screen.findByTestId("component-page");
    expect(page.textContent).toBe(`/web/components/${uuidFor("szp-c-bar")}?zoom=1`);
  });
});

describe("the KPI tiles (#790)", () => {
  it("renders one tile per contract metric with the effective value, sampled or default", () => {
    mount(undefined, health, [
      { metric_type_name: "room-temperature", label: "Room Temperature", data_type: "float", value: 23.5, is_sampled: true, from_contract: true, required: false },
      { metric_type_name: "occupancy-count", label: "Occupancy Count", data_type: "int", value: 0, is_sampled: false, from_contract: true, required: false },
    ]);
    const tiles = screen.getByTestId("kpi-tiles");
    expect(within(tiles).getByText("Room Temperature")).toBeTruthy();
    expect(within(tiles).getByText("23.5")).toBeTruthy();
    expect(within(tiles).getByText(/default/)).toBeTruthy();
  });

  it("renders no tile row at all when the standard declares nothing", () => {
    mount();
    expect(screen.queryByTestId("kpi-tiles")).toBeNull();
  });
});

describe("the map tab (#791)", () => {
  const MAPPED = [{
    id: uuidFor("szp-std"), name: "huddle-room", label: "Huddle Room", official: false,
    map: { aspect: 1.5, positions: [
      { role: "room-mic", position: 1, x: 0.32, y: 0.52 },
      { role: "room-mic", position: 2, x: 0.68, y: 0.52 },
      { role: "main-display", position: 1, x: 0.5, y: 0.06 },
    ] },
  }];

  it("a standard with a map yields the tab rail; without one there is no rail at all", () => {
    mount(undefined, health, [], MAPPED);
    expect(screen.getByTestId("tab-rail")).toBeTruthy();
    cleanup();
    mount();
    expect(screen.queryByTestId("tab-rail")).toBeNull();
  });

  it("the map tab renders one marker per declared position of the build in use, occupants solid and gaps hollow", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?zoom=1&tab=map`, health, [], MAPPED);
    const map = screen.getByTestId("system-map");
    const markers = within(map).getAllByTestId(/^mapmarker-/);
    expect(markers).toHaveLength(3);
    expect(within(map).getByText(/Room Microphone 2 · mic-1/)).toBeTruthy();
    expect(within(map).getByText(/Main Display · empty/)).toBeTruthy();
  });

  it("clicking an occupied marker opens the leaf, keeping the zoom", async () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?zoom=1&tab=map`, health, [], MAPPED);
    fireEvent.click(screen.getByTestId(`mapmarker-room-mic-2`));
    const page = await screen.findByTestId("component-page");
    expect(page.textContent).toContain(`/web/components/${uuidFor("szp-c-mic")}`);
  });
});

describe("the name-shaped address (#759's rule)", () => {
  it("keeps every search param through the uuid resolve, the tab included", async () => {
    mount(`/web/systems/boardroom?zoom=1&tab=map`, health, [], [
      { id: uuidFor("szp-std"), name: "huddle-room", label: "Huddle Room", official: false,
        map: { aspect: 1.5, positions: [{ role: "room-mic", position: 1, x: 0.3, y: 0.5 }] } },
    ]);
    await screen.findByTestId("system-map");
    expect(window.location.search).toContain("tab=map");
    expect(window.location.pathname).toContain(uuidFor("szp-sys"));
  });
});
