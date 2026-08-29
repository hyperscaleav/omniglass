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

// The fleet EntityBlade (#799, refit in #826): verdict and since lead, the
// alarms say why, and the rest of the blade IS the one EntityForm, read or
// edit through the blade's own footer. Expand promotes to the workspace,
// where the members, the strip, and the vitals live. Every body self-fetches
// by id, so the registry serves any page.

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
  qc.setQueryData([...SYSTEMS_KEY], [{ id: uuidFor("eb-sys"), name: "boardroom", label: "Boardroom", standard: "huddle-room", actions: ["update", "rename"] }]);
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
  it("leads with verdict and since, says why, and renders the form's sections", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    expect(screen.getAllByText("Boardroom").length).toBeGreaterThan(0);
    expect(screen.getAllByText("degraded").length).toBeGreaterThan(0);
    expect(screen.getByText(/since /)).toBeTruthy();
    expect(screen.getAllByText("No route to host").length).toBeGreaterThan(0);
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Identity")).toBeTruthy();
    expect(within(form).getByText("huddle-room")).toBeTruthy();
    expect(screen.queryByTestId("quick-name")).toBeNull();
  });

  it("expands to the identity route and closes the stack", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    fireEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("system-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/systems/${uuidFor("eb-sys")}`);
  });

  it("edits the whole form in place through the blade's footer", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const blade = await screen.findByRole("dialog");
    await within(blade).findByTestId("entity-form");
    fireEvent.click(within(blade).getByRole("button", { name: "Edit" }));
    expect(await within(blade).findByRole("combobox", { name: /standard/i })).toBeTruthy();
    // The rename precheck sits beside the name because the row allows rename.
    expect(within(blade).getByRole("button", { name: /check/i })).toBeTruthy();
    expect(within(blade).getByRole("button", { name: "Save" })).toBeTruthy();
  });

  it("keeps tags editable in place on the blade", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const blade = await screen.findByRole("dialog");
    const form = await within(blade).findByTestId("entity-form");
    expect(within(form).getByText("Tags")).toBeTruthy();
  });
});

describe("the component blade", () => {
  it("leads with verdict, says why, and renders the form with the fixed product", async () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    expect(screen.getAllByText("mic-1").length).toBeGreaterThan(0);
    expect(screen.getAllByText("outage").length).toBeGreaterThan(0);
    expect(screen.getAllByText("No route to host").length).toBeGreaterThan(0);
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getAllByText(/Fixed at creation/).length).toBeGreaterThan(0);
  });

  it("expands to the leaf route", async () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    fireEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("component-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/components/${uuidFor("eb-c-mic")}`);
  });
});

describe("the location blade", () => {
  it("leads with the verdict and renders the form with the parent", async () => {
    mountBlade({ kind: "location", id: uuidFor("eb-room") });
    expect(screen.getAllByText("Boardroom A").length).toBeGreaterThan(0);
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Parent")).toBeTruthy();
    expect(within(form).getByText("Root")).toBeTruthy();
  });
});
