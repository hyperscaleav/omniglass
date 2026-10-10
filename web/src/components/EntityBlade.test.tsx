import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, within, cleanup, waitFor } from "@solidjs/testing-library";
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
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { uuidFor } from "../lib/testids";

// The fleet EntityBlade (#799, refit in #826, the glance since #872): verdict
// and since lead, the alarms say why with their severity, the context the
// operator came for follows (a system's place, a component's systems and
// roles, a place's systems), then the form's identity, placement and tags,
// read or edit through the blade's own footer. Configuration (roles,
// properties, their cascade) is the detail view's Configure tab, one Expand
// away. Every body self-fetches by id, so the registry serves any page.

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
  qc.setQueryData([...LOCATION_TYPES_KEY], [{ id: uuidFor("ebt-room"), name: "room", label: "Room", allowed_parent_types: [] }]);
  qc.setQueryData([...STANDARDS_KEY], [{ id: uuidFor("eb-std"), name: "huddle-room", label: "Huddle Room Standard" }]);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], [
    { id: uuidFor("eb-c-mic"), name: "mic-1", label: "", component_type: "ceiling-mic", location_id: uuidFor("eb-room"), actions: ["update"] },
    { id: uuidFor("eb-c-bar"), name: "videobar-1", label: "", component_type: "video-bar", actions: [] },
  ]);
  qc.setQueryData([...componentAlarmsKey(uuidFor("eb-c-mic"))], [
    { id: "al-1", severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z", active: true },
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

afterEach(cleanup);

describe("the system blade", () => {
  it("leads with verdict and since, and says why with the severity and the component", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const blade = await screen.findByRole("dialog");
    expect(within(blade).getAllByText("degraded").length).toBeGreaterThan(0);
    expect(within(blade).getByText(/since /)).toBeTruthy();
    const why = within(blade).getByTestId("blade-why");
    expect(within(why).getByText("critical")).toBeTruthy();
    expect(within(why).getByText("No route to host")).toBeTruthy();
    expect(within(why).getByRole("button", { name: "mic-1" })).toBeTruthy();
  });

  // Each fact once (#872 drawer audit). The room's only system IS the room,
  // so its place card names no place (the title does), and offers no second
  // drawer for the room (its form is Configure place); the brief says the
  // size, not the standard the form's Classification already says; the
  // read-only form drops the Label row the title already says.
  it("says each fact once: no place name, no place drawer, no standard twice, no label row", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const blade = await screen.findByRole("dialog");
    const card = within(blade).getByTestId("place-card");
    expect(within(card).queryByText("Boardroom A")).toBeNull();
    expect(within(card).queryByRole("button", { name: "Place details" })).toBeNull();
    expect(within(card).getByRole("link", { name: "Configure place" })).toBeTruthy();
    const brief = within(blade).getByTestId("blade-brief");
    expect(within(brief).getByText("2 components")).toBeTruthy();
    expect(within(brief).queryByText("Huddle Room Standard")).toBeNull();
    const form = await within(blade).findByTestId("entity-form");
    expect(within(form).queryByText("Label")).toBeNull();
    expect(within(form).getAllByText("Huddle Room Standard").length).toBe(1);
  });

  it("renders the form's identity, placement and tags, and leaves the roles to Configure", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Identity")).toBeTruthy();
    expect(within(form).getByText("Tags")).toBeTruthy();
    expect(within(form).queryByText("Roles")).toBeNull();
  });

  it("expands to the identity route and closes the stack", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    fireEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("system-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/systems/${uuidFor("eb-sys")}`);
  });

  it("edits identity in place through the blade's footer", async () => {
    mountBlade({ kind: "system", id: uuidFor("eb-sys") });
    const blade = await screen.findByRole("dialog");
    await within(blade).findByTestId("entity-form");
    fireEvent.click(within(blade).getByRole("button", { name: "Edit" }));
    expect(await within(blade).findByRole("combobox", { name: /standard/i })).toBeTruthy();
    expect(within(blade).getByRole("button", { name: /check/i })).toBeTruthy();
    expect(within(blade).getByRole("button", { name: "Save" })).toBeTruthy();
  });
});

describe("the component blade", () => {
  it("leads with verdict, says why with severity, and names the systems it serves", async () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    const blade = await screen.findByRole("dialog");
    expect(within(blade).getAllByText("outage").length).toBeGreaterThan(0);
    const why = within(blade).getByTestId("blade-why");
    expect(within(why).getByText("critical")).toBeTruthy();
    expect(within(why).getByText("No route to host")).toBeTruthy();
    const serves = within(blade).getByTestId("blade-serves");
    expect(within(serves).getByText("Boardroom")).toBeTruthy();
    expect(await within(serves).findByText("Room Microphone")).toBeTruthy();
  });

  // The component sits in the room its one system makes: "Serves Boardroom"
  // already names that room, so a place card would name it twice.
  it("shows no place card where its place is the room of the system it serves, and keeps the product's note in a tooltip", async () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    const blade = await screen.findByRole("dialog");
    await within(blade).findByTestId("blade-serves");
    expect(within(blade).queryByTestId("place-card")).toBeNull();
    await within(blade).findByTestId("entity-form");
    expect(within(blade).queryByText(/Fixed at creation/)).toBeNull();
  });

  it("expands to the leaf route", async () => {
    mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    fireEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("component-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/components/${uuidFor("eb-c-mic")}`);
  });
});

describe("the component blade's place, by its primary system", () => {
  // Memberships come back by system name, not primary first: a component with
  // no place of its own must show its PRIMARY system's place, as its page does.
  it("shows the primary system's place for a component placed nowhere of its own", async () => {
    const r = mountBlade({ kind: "component", id: uuidFor("eb-c-mic") });
    r.qc.setQueryData([...COMPONENTS_KEY], [{ id: uuidFor("eb-c-mic"), name: "mic-1", label: "", actions: [] }]);
    r.qc.setQueryData([...FLEET_VIEW_KEY], {
      ...view,
      locations: [...(view.locations ?? []), { id: uuidFor("eb-other"), name: "annex", label: "Annex", location_type: "room", parent: "", verdict: "healthy" }],
      systems: [...(view.systems ?? []), { id: uuidFor("eb-a-sys"), name: "aaa", label: "Annex", location: uuidFor("eb-other"), verdict: "healthy", dots: [] }],
    });
    r.qc.setQueryData([...componentSystemsKey(uuidFor("eb-c-mic"))], [
      { system_id: uuidFor("eb-a-sys"), system: "aaa", primary: false },
      { system_id: uuidFor("eb-sys"), system: "boardroom", primary: true },
    ]);
    const blade = await screen.findByRole("dialog");
    // Primary first among the systems it serves.
    await waitFor(() => expect(within(blade).getByTestId("blade-serves").querySelector("button")?.textContent).toBe("Boardroom"));
  });
});

describe("the location blade", () => {
  it("says what the place holds: its systems, each with its verdict, opening the system", async () => {
    mountBlade({ kind: "location", id: uuidFor("eb-room") });
    const blade = await screen.findByRole("dialog");
    const here = within(blade).getByTestId("blade-systems");
    expect(within(here).getByText("Boardroom")).toBeTruthy();
    expect(within(here).getByText("degraded")).toBeTruthy();
    fireEvent.click(within(here).getByRole("button", { name: /Boardroom/ }));
    expect(await screen.findByRole("dialog", { name: "Boardroom" })).toBeTruthy();
  });

  // "Holds: Empty" beside a list of the room's systems was wrong: it counts
  // only the places beneath. A place holding systems and no places says
  // nothing about places at all.
  it("says nothing about places beneath a room that holds systems and no places", async () => {
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
    expect(within(form).queryByText("Properties")).toBeNull();
  });
});
