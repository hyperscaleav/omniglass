import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, fireEvent, within, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import BladeStack from "./BladeStack";
import { BladesContext, createBladeController, type BladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { systemSlotsKey } from "../lib/system_slots";
import { systemMetricsKey } from "../lib/system_metrics";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { LOCATIONS_KEY } from "../lib/locations";
import { componentAlarmsKey } from "../lib/alarms";
import { componentSystemsKey } from "../lib/members";
import { ME_KEY, type Me } from "../lib/auth";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { uuidFor } from "../lib/testids";
import { seedSeverityLevels } from "../lib/testseverity";

// The fleet EntityBlade (#799, refit in #826, the glance since #872): health
// and since lead, the alarms say why with their severity, the context the
// operator came for follows (a system's location, a component's systems and
// slots, a location's systems), then the form's identity, placement and tags,
// read or edit through the blade's own footer. Configuration (slots,
// properties, their cascade) is the detail view's Configure tab, one Expand
// away. Every body self-fetches by id, so the catalog serves any page.

const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };

const view: FleetView = {
  locations: [
    { id: uuidFor("eb-floor"), name: "level-2", label: "Level 2", location_type: "floor", location_type_id: uuidFor("ebt-floor"), parent: "", health: "degraded" },
    { id: uuidFor("eb-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", location_type_id: uuidFor("ebt-room"), parent: uuidFor("eb-floor"), health: "degraded" },
  ],
  systems: [
    {
      id: uuidFor("eb-sys"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("eb-room"),
      health: "degraded",
      dots: [
        { component: uuidFor("eb-c-bar"), name: "videobar-1", health: "healthy", primary: true, shared: false },
        { component: uuidFor("eb-c-mic"), name: "mic-1", health: "outage", primary: true, shared: false },
      ],
    },
  ],
} as unknown as FleetView;

const health: FleetHealth = {
  health: "degraded",
  slots: [
    {
      name: "room-mic", label: "Room Microphone", impact: "degraded", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: true, assigned_to: ["mic-1"], down: ["mic-1"],
      alarms: [{ id: "al-1", component: "mic-1", component_id: uuidFor("eb-c-mic"), severity: "high", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" }],
    },
  ],
  systems: [],
  transitions: [
    { ts: "2026-08-01T09:00:00Z", health: "healthy" },
    { ts: "2026-08-15T14:20:00Z", health: "degraded" },
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
  seedSeverityLevels(qc);
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...systemHealthKey(uuidFor("eb-sys"))], health);
  qc.setQueryData([...systemSlotsKey(uuidFor("eb-sys"))], declared);
  qc.setQueryData([...systemMetricsKey(uuidFor("eb-sys"))], [
    { metric_type_name: "room-temperature", label: "Room temperature", value: "22.5", is_sampled: true, from_contract: false },
  ]);
  qc.setQueryData([...SYSTEMS_KEY], [{ id: uuidFor("eb-sys"), name: "boardroom", label: "Boardroom", standard: "huddle-room", actions: ["update", "rename"] }]);
  qc.setQueryData([...LOCATIONS_KEY], [
    { id: uuidFor("eb-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", actions: ["update"] },
  ]);
  qc.setQueryData([...LOCATION_TYPES_KEY], [{ id: uuidFor("ebt-room"), name: "room", label: "Room", allowed_parent_types: [] }]);
  qc.setQueryData([...STANDARDS_KEY], [{ id: uuidFor("eb-std"), name: "huddle-room", label: "Huddle Room Standard" }]);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], [
    { id: uuidFor("eb-c-mic"), name: "mic-1", label: "", component_type: "ceiling-mic", location_id: uuidFor("eb-room"), actions: ["update"] },
    { id: uuidFor("eb-c-bar"), name: "videobar-1", label: "", component_type: "video-bar", actions: [] },
  ]);
  qc.setQueryData([...componentAlarmsKey(uuidFor("eb-c-mic"))], [
    { id: "al-1", component: "mic-1", component_id: uuidFor("eb-c-mic"), severity: "high", message: "No route to host", raised_at: "2026-08-15T14:20:00Z", active: true, raised_by_rule: "icmp-unreachable", raised_by_rule_id: uuidFor("eb-rule") },
  ]);
  qc.setQueryData([...componentSystemsKey(uuidFor("eb-c-mic"))], [{ system_id: uuidFor("eb-sys"), system: "boardroom", primary: true }]);

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
  return { ...result, controller: () => controller, qc };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("the location blade", () => {
  it("says what the location holds: its systems, each with its health, opening the system", async () => {
    mountBlade({ kind: "location", id: uuidFor("eb-room") });
    const blade = await screen.findByRole("dialog");
    const here = within(blade).getByTestId("blade-systems");
    expect(within(here).getByText("Boardroom")).toBeTruthy();
    expect(within(here).getByText("degraded")).toBeTruthy();
    fireEvent.click(within(here).getByRole("button", { name: /Boardroom/ }));
    expect(await screen.findByRole("dialog", { name: "Boardroom" })).toBeTruthy();
  });

  // "Holds: Empty" beside a list of the room's systems was wrong: it counts
  // only the locations beneath. A location holding systems and no locations says
  // nothing about locations at all.
  it("says nothing about locations beneath a room that holds systems and no locations", async () => {
    mountBlade({ kind: "location", id: uuidFor("eb-room") });
    const blade = await screen.findByRole("dialog");
    await within(blade).findByTestId("blade-systems");
    expect(within(blade).queryByTestId("blade-contents")).toBeNull();
  });

  it("renders the form with the parent", async () => {
    mountBlade({ kind: "location", id: uuidFor("eb-room") });
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Parent")).toBeTruthy();
    expect(within(form).getByText("Root")).toBeTruthy();
  });
});

