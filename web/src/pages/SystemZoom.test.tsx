import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Systems from "./Systems";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { systemRolesKey } from "../lib/system_roles";
import { SYSTEMS_KEY } from "../lib/systems";
import { systemMetricsKey } from "../lib/system_metrics";
import { componentAlarmsKey } from "../lib/alarms";
import { systemEventsKey, systemLogsKey } from "../lib/system_activity";
import { metricSeriesKey } from "../lib/series";
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
// than an error. The identity route's default face (ADR-0129).

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

function mount(path = `/web/systems/${uuidFor("szp-sys")}`, healthOverride: FleetHealth = health, metrics: unknown[] = [], standards: unknown[] = [], meOverride: Me = me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], meOverride);
  qc.setQueryData([...SYSTEMS_KEY], [{ id: uuidFor("szp-sys"), name: "boardroom", label: "Boardroom", standard: "huddle-room" }]);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemHealthKey(uuidFor("szp-sys"))], healthOverride);
  qc.setQueryData([...systemRolesKey(uuidFor("szp-sys"))], declared);
  qc.setQueryData([...systemMetricsKey(uuidFor("szp-sys"))], metrics);
  qc.setQueryData(["system-properties", uuidFor("szp-sys")], [
    { property_type_name: "room-owner", label: "Room owner", value: "\"facilities\"", from_contract: true, source: "default" },
    { property_type_name: "asset-tag", label: "Asset tag", value: "\"A-100\"", from_contract: false, source: "self" },
  ]);
  qc.setQueryData([...STANDARDS_KEY], standards);
  window.history.pushState({}, "", path);
  const r = render(() => (

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
  return Object.assign(r, { qc });
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

  it("a no-role member's card opens its component blade too (#799)", async () => {
    mount();
    fireEvent.click(screen.getByTestId(`compcard-${uuidFor("szp-c-power")}`));
    const blade = await screen.findByRole("dialog");
    expect(blade.getAttribute("aria-labelledby")).toBe(`blade-title-component-${uuidFor("szp-c-power")}`);
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

  it("a legacy ?zoom=1 deep link still lands on the workspace: old links never break", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?zoom=1`);
    expect(screen.getByTestId("system-header")).toBeTruthy();
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

  it("clicking a card opens the component blade; Expand promotes to the leaf (#799)", async () => {
    mount();
    fireEvent.click(screen.getByTestId(`compcard-${uuidFor("szp-c-bar")}`));
    const blade = await screen.findByRole("dialog");
    fireEvent.click(within(blade).getByRole("button", { name: "Expand" }));
    const page = await screen.findByTestId("component-page");
    expect(page.textContent).toBe(`/web/components/${uuidFor("szp-c-bar")}`);
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

  it("a standard with a map yields the Map tab; without one the tab is absent (History keeps the rail)", () => {
    mount(undefined, health, [], MAPPED);
    expect(screen.getByRole("tab", { name: "Map" })).toBeTruthy();
    cleanup();
    mount();
    expect(screen.getByTestId("tab-rail")).toBeTruthy();
    expect(screen.queryByRole("tab", { name: "Map" })).toBeNull();
  });

  it("the map tab renders one marker per declared position of the build in use, occupants solid and gaps hollow", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?tab=map`, health, [], MAPPED);
    const map = screen.getByTestId("system-map");
    const markers = within(map).getAllByTestId(/^mapmarker-/);
    expect(markers).toHaveLength(3);
    expect(within(map).getByText(/Room Microphone 2 · mic-1/)).toBeTruthy();
    expect(within(map).getByText(/Main Display · empty/)).toBeTruthy();
  });

  it("clicking an occupied marker opens its component blade (#799)", async () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?tab=map`, health, [], MAPPED);
    fireEvent.click(screen.getByTestId(`mapmarker-room-mic-2`));
    const blade = await screen.findByRole("dialog");
    expect(blade.getAttribute("aria-labelledby")).toBe(`blade-title-component-${uuidFor("szp-c-mic")}`);
  });
});

describe("the name-shaped address (#759's rule)", () => {
  it("keeps every search param through the uuid resolve, the tab included", async () => {
    mount(`/web/systems/boardroom?tab=map`, health, [], [
      { id: uuidFor("szp-std"), name: "huddle-room", label: "Huddle Room", official: false,
        map: { aspect: 1.5, positions: [{ role: "room-mic", position: 1, x: 0.3, y: 0.5 }] } },
    ]);
    await screen.findByTestId("system-map");
    expect(window.location.search).toContain("tab=map");
    expect(window.location.pathname).toContain(uuidFor("szp-sys"));
  });
});

describe("the history tab (#792)", () => {
  function seedAlarms(qc: QueryClient) {
    qc.setQueryData([...componentAlarmsKey(uuidFor("szp-c-mic"))], [
      { id: "hal-1", component: uuidFor("szp-c-mic"), severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z", active: true, acknowledged: false },
    ]);
    qc.setQueryData([...componentAlarmsKey(uuidFor("szp-c-bar"))], [
      { id: "hal-0", component: uuidFor("szp-c-bar"), severity: "warning", message: "Fan speed high", raised_at: "2026-08-09T09:12:00Z", cleared_at: "2026-08-09T09:53:00Z", active: false, acknowledged: true },
    ]);
    qc.setQueryData([...componentAlarmsKey(uuidFor("szp-c-power"))], []);
  }

  // #795 refinement: the tab reads like a status page: uptime up top, the
  // timeline, then each unhealthy stretch as an incident whose reasoning
  // expands. The old flat what-went-wrong assertions live on inside the
  // incident form below.
  it("leads with the window's uptime beside the timeline", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?tab=history`);
    const tab = screen.getByTestId("history-tab");
    const uptime = within(tab).getByTestId("uptime-kpi");
    expect(uptime.textContent).toMatch(/\d+(\.\d)?%/);
    expect(within(tab).getByTestId("health-history-full")).toBeTruthy();
  });

  it("renders the ongoing unhealthy stretch as the first incident, expandable to the alarms that explain it", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=history`);
    seedAlarms(r.qc);
    const list = screen.getByTestId("incident-list");
    const first = within(list).getAllByTestId(/^incident-/)[0];
    expect(within(first).getByText(/ongoing/)).toBeTruthy();
    fireEvent.click(within(first).getByRole("button", { name: /expand/ }));
    expect(await within(first).findByText("No route to host")).toBeTruthy();
    expect(within(first).getAllByText(/degraded/).length).toBeGreaterThan(0);
  });

  it("an alarm outside every incident still shows, under other alarms", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=history`);
    seedAlarms(r.qc);
    const tab = screen.getByTestId("history-tab");
    expect(await within(tab).findByText("Fan speed high")).toBeTruthy();
  });

  it("is always on the rail, with the timeline and raise markers", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=history`);
    seedAlarms(r.qc);
    expect(screen.getByRole("tab", { name: "History" })).toBeTruthy();
    expect(screen.getByTestId("health-history-full")).toBeTruthy();
    await screen.findByText("Fan speed high");
  });

  it("marks each raise on the strip's axis", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=history`);
    seedAlarms(r.qc);
    await screen.findByText("No route to host");
    expect(screen.getAllByTestId(/^incident-marker-/)).toHaveLength(2);
  });
});

describe("the events and logs tabs (#793)", () => {
  it("the events tab lists the room's story newest first, each row labeled by its owner", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=events`);
    r.qc.setQueryData([...systemEventsKey(uuidFor("szp-sys"))], [
      { ts: "2026-08-18T15:53:00Z", key: "call-started", event_type_id: "et-1", origin: "caught", message: "call started", provenance: "observed", owner_kind: "component", owner: "videobar-1" },
      { ts: "2026-08-18T15:50:00Z", key: "occupancy-changed", event_type_id: "et-2", origin: "derived", message: "0 to 6", provenance: "derived", owner_kind: "system", owner: "boardroom" },
    ]);
    const tab = screen.getByTestId("events-tab");
    expect(await within(tab).findByText("call started")).toBeTruthy();
    expect(within(tab).getByText("videobar-1")).toBeTruthy();
    expect(within(tab).getByText("occupancy-changed")).toBeTruthy();
    expect(within(tab).getByText("boardroom")).toBeTruthy();
  });

  it("the logs tab renders the members' lines with severity colouring the row, and an empty room says so", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=logs`);
    r.qc.setQueryData([...systemLogsKey(uuidFor("szp-sys"))], [
      { ts: "2026-08-18T16:45:02Z", severity: "error", message: "connect timeout", component: "videobar-1" },
      { ts: "2026-08-18T16:42:11Z", severity: "info", message: "qrc poll ok", component: "dsp" },
    ]);
    const tab = screen.getByTestId("logs-tab");
    expect(await within(tab).findByText(/connect timeout/)).toBeTruthy();
    expect(within(tab).getByText(/qrc poll ok/)).toBeTruthy();
    cleanup();
    const r2 = mount(`/web/systems/${uuidFor("szp-sys")}?tab=logs`);
    r2.qc.setQueryData([...systemLogsKey(uuidFor("szp-sys"))], []);
    expect(await screen.findByText(/No lines in the window/)).toBeTruthy();
  });
});

describe("the data tab (#794, stacked per the #795 review)", () => {
  const METRICS = [
    { metric_type_name: "room-temperature", label: "Room Temperature", data_type: "float", value: 23.5, is_sampled: true, from_contract: true, required: false },
    { metric_type_name: "occupancy-count", label: "Occupancy Count", data_type: "int", value: 0, is_sampled: false, from_contract: true, required: false },
  ];
  const seedSeries = (qc: QueryClient) => {
    qc.setQueryData([...metricSeriesKey("systems", uuidFor("szp-sys"), "room-temperature", 24)], [
      { ts: "2026-08-20T10:00:00Z", value: 22.9, provenance: "observed" },
      { ts: "2026-08-20T14:00:00Z", value: 23.5, provenance: "observed" },
    ]);
    qc.setQueryData([...metricSeriesKey("systems", uuidFor("szp-sys"), "occupancy-count", 24)], []);
  };

  it("stacks every declared metric as a table row: label, sparkline, the latest value; no picker to hunt through", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=data`, health, METRICS);
    seedSeries(r.qc);
    const tab = screen.getByTestId("data-tab");
    const temp = within(tab).getByTestId("metric-row-room-temperature");
    expect(await within(temp).findByTestId("sparkline")).toBeTruthy();
    expect(within(temp).getByText("23.5")).toBeTruthy();
    const occ = within(tab).getByTestId("metric-row-occupancy-count");
    expect(within(occ).getByText(/contract default/)).toBeTruthy();
    expect(within(tab).queryByTestId("timeseries-chart")).toBeNull();
  });

  it("a row expands to the full chart and collapses back", async () => {
    const r = mount(`/web/systems/${uuidFor("szp-sys")}?tab=data`, health, METRICS);
    seedSeries(r.qc);
    fireEvent.click(screen.getByTestId("metric-row-room-temperature"));
    expect(await screen.findByTestId("timeseries-chart")).toBeTruthy();
    fireEvent.click(screen.getByTestId("metric-row-room-temperature"));
    expect(screen.queryByTestId("timeseries-chart")).toBeNull();
  });

  it("hides the tab with nothing declared", () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?tab=data`, health, []);
    expect(screen.queryByRole("tab", { name: "Data" })).toBeNull();
  });
});

describe("the scoped summary (#795 review)", () => {
  it("talks about THIS system's components: mix subject, slots, alarms", () => {
    mount();
    const rail = screen.getByTestId("fleet-summary");
    expect(within(rail).getByText("components")).toBeTruthy();
    expect(within(rail).queryByText("roots")).toBeNull();
    expect(within(rail).getByText("slots filled")).toBeTruthy();
    expect(within(rail).getByText(/active alarms?/)).toBeTruthy();
  });
});


// The Configure facet (#800 slice 1): editing is one more tab of the
// workspace. Identity, Classification, Placement, Tags, the classic face's
// own save contract (update first, rename last, invalidate in finally), and
// the tab renders only for a caller who could change anything.
describe("the configure tab (#800)", () => {
  it("offers Configure to an owner and renders the sections from the row", async () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    expect(within(face).getByText("Identity")).toBeTruthy();
    expect(within(face).getByText("Classification")).toBeTruthy();
    expect(within(face).getByText("Placement")).toBeTruthy();
    expect(within(face).getAllByText("Tags").length).toBeGreaterThan(0);
    // The row's facts read back before any edit: the name in the data font,
    // the standard it conforms to.
    expect(within(face).getByText("boardroom")).toBeTruthy();
    expect(within(face).getAllByText(/huddle-room/).length).toBeGreaterThan(0);
  });

  it("saves the classic contract: label PATCHes by uuid, rename goes last", async () => {
    const calls: { method: string; url: string; body: string }[] = [];
    const stub = vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
      const req = input as Request;
      const body = req.method === "PATCH" || req.method === "POST" ? await req.clone().text() : "";
      calls.push({ method: req.method, url: req.url, body });
      return new Response(JSON.stringify({}), { status: 200, headers: { "content-type": "application/json" } });
    });
    try {
      mount(`/web/systems/${uuidFor("szp-sys")}?tab=configure`);
      const face = await screen.findByTestId("configure-face");
      fireEvent.click(within(face).getByRole("button", { name: "Edit" }));
      const name = await within(face).findByDisplayValue("boardroom");
      fireEvent.input(name, { target: { value: "exec-boardroom" } });
      fireEvent.click(within(face).getByRole("button", { name: /save/i }));
      await waitFor(() => {
        expect(within(face).queryByRole("alert")?.textContent ?? "", "save error surfaced").toBe("");
        const patch = calls.find((c) => c.method === "PATCH" && c.url.includes(`/systems/${uuidFor("szp-sys")}`));
        expect(patch, "the update PATCH by uuid").toBeTruthy();
        const rename = calls.find((c) => c.method === "POST" && c.url.includes(":rename"));
        expect(rename, "the rename custom method; saw " + JSON.stringify(calls.map((c) => c.method + " " + c.url))).toBeTruthy();
        expect(JSON.parse(rename!.body).name).toBe("exec-boardroom");
      });
    } finally {
      stub.mockRestore();
    }
  });

  it("hides Configure from a caller with no edit verb", async () => {
    const viewer: Me = { principal: { id: "u-v", kind: "human" }, human: { username: "v" }, permissions: ["system:read", "location:read", "component:read", "standard:read", "system_type:read", "metric_type:read"], grants: [] };
    mount(`/web/systems/${uuidFor("szp-sys")}`, health, [], [], viewer);
    await screen.findByTestId("tab-rail");
    expect(screen.queryByRole("tab", { name: "Configure" })).toBeNull();
  });
});


// Slice 2 of #800: ?edit=1 means the one editor, wherever it is typed. The
// bare param lands the workspace on Configure, already editing.
describe("?edit=1 lands on configure (#800)", () => {
  it("opens the configure tab editing from a bare ?edit=1 address", async () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?edit=1`);
    const face = await screen.findByTestId("configure-face");
    expect(await within(face).findByDisplayValue("boardroom")).toBeTruthy();
    expect(within(face).getByRole("button", { name: /save/i })).toBeTruthy();
  });
});


// The properties surface the classic face carried, re-homed on Configure
// (#800 slice 3): the standard's contract resolves with off-contract values
// apart, on the workspace.
describe("properties live on configure (#800)", () => {
  it("resolves the contract on the configure face", async () => {
    mount(`/web/systems/${uuidFor("szp-sys")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    expect(await within(face).findByText("Room owner")).toBeTruthy();
    expect(within(face).getByText("Asset tag")).toBeTruthy();
  });
});

describe("the miss face (#800)", () => {
  it("an address matching no system renders the explicit miss, not a silent fallback", async () => {
    mount("/web/systems/no-such-room");
    expect(await screen.findByText(/No system answers this address/)).toBeTruthy();
    expect(screen.queryByText("Room Microphone")).toBeNull();
  });
});
