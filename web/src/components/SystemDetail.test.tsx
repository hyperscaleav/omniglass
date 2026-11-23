import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SystemZoom from "../pages/SystemZoom";
import BladeStack from "./BladeStack";
import { BladesContext, createBladeController, type BladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { systemSlotsKey } from "../lib/system_slots";
import { systemMetricsKey } from "../lib/system_metrics";
import { systemMembersKey } from "../lib/members";
import { systemAlarmsKey } from "../lib/alarms";
import { systemEventsKey, systemLogsKey } from "../lib/system_activity";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { COMPONENT_TYPES_KEY } from "../lib/component_types";
import { SYSTEM_TYPES_KEY } from "../lib/system_types";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { TAGS_KEY } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { pendingTriage, requestTriage } from "../lib/handover";
import { uuidFor } from "../lib/testids";
import { seedSeverityLevels } from "../lib/testseverity";
import { answerConfirms } from "../lib/confirm.testkit";

// SystemDetail (#2423): the system page and the system blade render ONE body,
// so a blade shows every section, tab and action its page shows.

const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const SID = uuidFor("sd-sys");
const MIC = uuidFor("sd-c-mic");

const view: FleetView = {
  locations: [
    { id: uuidFor("sd-floor"), name: "level-2", label: "Level 2", location_type: "floor", parent: "", health: "degraded" },
    { id: uuidFor("sd-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", parent: uuidFor("sd-floor"), health: "degraded" },
  ],
  systems: [
    {
      id: SID, name: "boardroom", label: "Boardroom", location: uuidFor("sd-room"), health: "degraded",
      dots: [{ component: MIC, name: "mic-1", health: "outage", primary: true, shared: false }],
    },
  ],
} as unknown as FleetView;

const health: FleetHealth = {
  health: "degraded",
  slots: [
    {
      name: "room-mic", label: "Room Microphone", impact: "degraded", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: true, assigned_to: ["mic-1"], down: ["mic-1"],
      alarms: [{ id: "al-1", component: "mic-1", component_id: MIC, severity: "high", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" }],
    },
  ],
  systems: [],
  transitions: [
    { ts: "2026-08-01T09:00:00Z", health: "healthy" },
    { ts: "2026-08-15T14:20:00Z", health: "degraded" },
  ],
} as unknown as FleetHealth;

function seed(actions: string[] = ["update", "delete", "rename"]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  seedSeverityLevels(qc);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...systemHealthKey(SID)], health);
  qc.setQueryData([...systemSlotsKey(SID)], [
    { name: "room-mic", label: "Room Microphone", quorum: 1, assigned: 1, impact: "degraded", from_standard: true, accepted_types: [], pinned_products: [], assigned_to: ["mic-1"], positions: [1], position_labels: [] },
  ]);
  qc.setQueryData([...systemMetricsKey(SID)], []);
  qc.setQueryData([...systemMembersKey(SID)], []);
  qc.setQueryData([...systemAlarmsKey(SID)], []);
  qc.setQueryData([...systemEventsKey(SID)], []);
  qc.setQueryData([...systemLogsKey(SID, {})], { logs: [], sources: [] });
  qc.setQueryData([...SYSTEMS_KEY], [{ id: SID, name: "boardroom", label: "Boardroom", standard: "huddle-room", location_id: uuidFor("sd-room"), actions }]);
  qc.setQueryData([...COMPONENTS_KEY], [{ id: MIC, name: "mic-1", label: "", actions: [] }]);
  qc.setQueryData([...COMPONENT_TYPES_KEY], []);
  qc.setQueryData([...SYSTEM_TYPES_KEY], []);
  qc.setQueryData([...LOCATIONS_KEY], [{ id: uuidFor("sd-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", actions: ["update"] }]);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], [{ id: uuidFor("sd-std"), name: "huddle-room", label: "Huddle Room Standard" }]);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  return qc;
}

function mountPage(path = `/web/systems/${SID}`, qc = seed()) {
  window.history.pushState({}, "", path);
  render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/systems/:id" component={SystemZoom} />
        <Route path="/explore" component={() => <div data-testid="explore-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

function mountBlade(path = "/web/explore", qc = seed()) {
  window.history.pushState({}, "", path);
  let controller!: BladeController;
  render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route
          path="/explore"
          component={() => {
            controller = createBladeController();
            controller.push({ kind: "system", id: SID });
            return (
              <BladesContext.Provider value={controller}>
                <BladeStack controller={controller} registry={fleetRegistry} />
              </BladesContext.Provider>
            );
          }}
        />
        <Route path="/systems/:id" component={() => <div data-testid="system-page" />} />
      </Router>
    </QueryClientProvider>
  ));
  return { qc, controller: () => controller };
}

const sections = (root: HTMLElement) => [...root.querySelectorAll("[data-testid]")].map((e) => e.getAttribute("data-testid")!).filter((t) => !t.startsWith("alarm-marker-"));

async function sectionsPerTab(root: () => HTMLElement) {
  const out: Record<string, string[]> = {};
  for (const tab of ["Overview", "Activity", "Configure"]) {
    fireEvent.click(within(root()).getByRole("tab", { name: tab }));
    await waitFor(() => expect(within(root()).getByRole("tab", { name: tab }).getAttribute("aria-selected")).toBe("true"));
    if (tab === "Configure") await waitFor(() => expect(within(root()).getAllByTestId("entity-form").length).toBe(2));
    out[tab] = sections(root());
  }
  return out;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("one body in two hosts", () => {
  it("the blade renders every section, on every tab, that the page renders", async () => {
    mountPage();
    const onPage = await sectionsPerTab(() => screen.getByTestId("system-detail"));
    cleanup();
    mountBlade();
    const blade = await screen.findByRole("dialog");
    await within(blade).findByTestId("system-detail");
    const inBlade = await sectionsPerTab(() => within(blade).getByTestId("system-detail"));
    // The blade's location card says where the room sits (no breadcrumb).
    expect(inBlade.Overview.filter((t) => t !== "location-path")).toEqual(onPage.Overview);
    expect(inBlade.Activity.filter((t) => t !== "location-path")).toEqual(onPage.Activity);
    expect(inBlade.Configure.filter((t) => t !== "location-path")).toEqual(onPage.Configure);
    expect(onPage.Overview).toEqual(expect.arrayContaining(["system-header", "tab-rail", "alarm-strip", "overview-tab"]));
    expect(onPage.Activity).toEqual(expect.arrayContaining(["history-tab", "events-tab", "logs-tab"]));
    expect(onPage.Configure).toEqual(expect.arrayContaining(["configure-face-location", "configure-face"]));
    expect(within(blade).getByTestId("location-path").textContent).toBe("Level 2");
    expect(blade.getAttribute("data-blade-width")).toBe("wide");
  });

  it("each Why row offers Acknowledge, the timeline and Troubleshoot in one group, in the blade too", async () => {
    mountBlade();
    const blade = await screen.findByRole("dialog");
    const why = await within(blade).findByTestId("alarm-strip");
    const controls = within(why).getByTestId("alarm-controls");
    expect(within(controls).getByRole("button", { name: /acknowledge/i })).toBeTruthy();
    expect(within(controls).getByRole("button", { name: /^Timeline of / })).toBeTruthy();
    fireEvent.click(within(controls).getByRole("button", { name: "Troubleshoot No route to host with omnibot" }));
    expect(pendingTriage()?.ref).toMatch(/^mic-1/);
    requestTriage(undefined);
  });

  it("a component opens over the system's blade, on the same stack", async () => {
    const r = mountBlade();
    const blade = await screen.findByRole("dialog");
    const why = await within(blade).findByTestId("alarm-strip");
    fireEvent.click(within(why).getByRole("button", { name: "mic-1" }));
    await waitFor(() => expect(r.controller().stack().map((x) => x.kind)).toEqual(["system", "component"]));
  });

  it("expands to the system page and closes the stack", async () => {
    mountBlade();
    fireEvent.click(await screen.findByRole("button", { name: "Expand" }));
    expect(await screen.findByTestId("system-page")).toBeTruthy();
  });
});

describe("the blade's tab and edit are its own", () => {
  it("switching tabs in a blade leaves the page's address untouched", async () => {
    mountBlade("/web/explore?node=x");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Activity" }));
    expect(within(blade).getByTestId("history-tab")).toBeTruthy();
    expect(window.location.search).toBe("?node=x");
  });

  it("edits the system from the blade's Configure, and a page ?edit=location does not reach it", async () => {
    mountBlade("/web/explore?edit=location");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    const face = await within(blade).findByTestId("configure-face");
    expect(within(blade).queryByDisplayValue("boardroom-a")).toBeNull();
    fireEvent.click(await within(face).findByRole("button", { name: "Edit" }));
    expect(await within(face).findByDisplayValue("boardroom")).toBeTruthy();
    expect(window.location.search).toBe("?edit=location");
  });

  it("deletes from the blade's Configure and closes the stack", async () => {
    answerConfirms(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(null, { status: 204 }));
    const r = mountBlade();
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    const face = await within(blade).findByTestId("configure-face");
    fireEvent.click(await within(face).findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(r.controller().stack()).toEqual([]));
  });
});
