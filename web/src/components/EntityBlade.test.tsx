import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, within, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import BladeStack from "./BladeStack";
import { BladesContext, createBladeController, type BladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { systemRolesKey } from "../lib/system_roles";
import { systemMetricsKey } from "../lib/system_metrics";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { LOCATIONS_KEY } from "../lib/locations";
import { componentAlarmsKey } from "../lib/alarms";
import { componentSystemsKey } from "../lib/members";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The fleet EntityBlade (#799): a condensed render of the workspace's cores.
// Verdict and since lead, the alarms say why, the members drill into the
// component blade in the same stack, Expand promotes to the identity route,
// and the label edits in place when the row allows update. Every body
// self-fetches by id, so the registry serves any page.

const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

const view: FleetView = {
  locations: [
    { id: uuidFor("eb-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", location_type_id: uuidFor("ebt-room"), parent: "", verdict: "degraded" },
  ],
  systems: [
    {
      id: uuidFor("eb-sys"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("eb-room"),
      verdict: "degraded",
      dots: [
        { component: uuidFor("eb-c-bar"), name: "videobar-1", verdict: "healthy", primary: true, shared: false },
        { component: uuidFor("eb-c-mic"), name: "mic-1", verdict: "outage", primary: true, shared: false },
      ],
    },
  ],
} as unknown as FleetView;

const health: FleetHealth = {
  verdict: "degraded",
  roles: [
    {
      name: "room-mic", label: "Room Microphone", impact: "degraded", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: true, assigned_to: ["mic-1"], down: ["mic-1"],
      alarms: [{ id: "al-1", component: uuidFor("eb-c-mic"), severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" }],
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
    name: "room-mic", label: "Room Microphone", quorum: 1, assigned: 1, impact: "degraded", from_standard: true,
    accepted_types: ["ceiling-mic"], pinned_products: [], assigned_to: ["mic-1"], positions: [1], position_labels: [],
  },
];

function mountBlade(ref: { kind: string; id: string }) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...systemHealthKey(uuidFor("eb-sys"))], health);
  qc.setQueryData([...systemRolesKey(uuidFor("eb-sys"))], declared);
  qc.setQueryData([...systemMetricsKey(uuidFor("eb-sys"))], [
    { metric_type_name: "room-temperature", label: "Room temperature", value: "22.5", is_sampled: true, from_contract: false },
  ]);
  qc.setQueryData([...SYSTEMS_KEY], [{ id: uuidFor("eb-sys"), name: "boardroom", label: "Boardroom", standard: "huddle-room", actions: ["update"] }]);
  qc.setQueryData([...LOCATIONS_KEY], [
    { id: uuidFor("eb-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", actions: ["update"] },
  ]);
  qc.setQueryData([...COMPONENTS_KEY], [
    { id: uuidFor("eb-c-mic"), name: "mic-1", label: "", component_type: "ceiling-mic", actions: ["update"] },
    { id: uuidFor("eb-c-bar"), name: "videobar-1", label: "", component_type: "video-bar", actions: [] },
  ]);
  qc.setQueryData([...componentAlarmsKey(uuidFor("eb-c-mic"))], [
    { id: "al-1", severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z", active: true },
  ]);
  qc.setQueryData([...componentSystemsKey(uuidFor("eb-c-mic"))], [{ system_id: uuidFor("eb-sys"), role: "room-mic" }]);

  window.history.pushState({}, "", "/web/fleet");
  let controller!: BladeController;
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route
          path="/fleet"
          component={() => {
            controller = createBladeController();
            controller.push(ref);
            return (
              <BladesContext.Provider value={controller}>
                <BladeStack controller={controller} registry={fleetRegistry} />
              </BladesContext.Provider>
            );
          }}
        />
        <Route path="/systems/:id" component={() => <div data-testid="system-page" />} />
        <Route path="/components/:id" component={() => <div data-testid="component-page" />} />
      </Router>
    </QueryClientProvider>
  ));
  return { ...result, controller: () => controller };
}

afterEach(cleanup);

describe("the system blade", () => {
  it("leads with verdict and since, says why, and lists the members", () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    expect(screen.getAllByText("Boardroom").length).toBeGreaterThan(0);
    expect(screen.getByText("degraded")).toBeTruthy();
    expect(screen.getByText(/since /)).toBeTruthy();
    expect(screen.getByText("No route to host")).toBeTruthy();
    expect(screen.getByText("videobar-1")).toBeTruthy();
    expect(screen.getByText(/Room temperature/i)).toBeTruthy();
  });

  it("expands to the identity route and closes the stack", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    fireEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("system-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/systems/${uuidFor("eb-sys")}`);
  });

  it("drills a member into the component blade on the same stack", async () => {
    const { controller } = mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const members = screen.getByText("Components").parentElement!;
    fireEvent.click(within(members).getByRole("button", { name: /mic-1/ }));
    expect(controller().stack().length).toBe(2);
    expect(controller().stack()[1]).toEqual({ kind: "component", id: uuidFor("eb-c-mic") });
  });

  it("offers the in-blade edit pencil when the row allows update", () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
  });
});

describe("the component blade", () => {
  it("leads with verdict, says why, and names the system it serves", () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    expect(screen.getAllByText("mic-1").length).toBeGreaterThan(0);
    expect(screen.getByText("outage")).toBeTruthy();
    expect(screen.getByText("No route to host")).toBeTruthy();
    expect(screen.getByText("Boardroom")).toBeTruthy();
  });

  it("expands to the leaf route", async () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    fireEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("component-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/components/${uuidFor("eb-c-mic")}`);
  });
});

describe("the location blade", () => {
  it("summarises the subtree and lists what needs attention", () => {
    mountBlade({ kind: "location", id: uuidFor("eb-room") });
    expect(screen.getAllByText("Boardroom A").length).toBeGreaterThan(0);
    expect(screen.getByText(/1 system/)).toBeTruthy();
    const rows = screen.getByText("Needs attention").parentElement!;
    expect(within(rows).getByText("Boardroom")).toBeTruthy();
  });
});
