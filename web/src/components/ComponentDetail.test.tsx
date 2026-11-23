import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ComponentLeaf from "../pages/ComponentLeaf";
import BladeStack from "./BladeStack";
import { BladesContext, createBladeController, type BladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { COMPONENTS_KEY } from "../lib/components";
import { componentSystemsKey } from "../lib/members";
import { REACHABILITY_KEY } from "../lib/reachability";
import { componentAlarmsReadKey } from "../lib/alarms";
import { effectivePropertiesKey } from "../lib/component_properties";
import { effectiveMetricsKey } from "../lib/component_metrics";
import { PRODUCTS_KEY } from "../lib/products";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { TAGS_KEY } from "../lib/tags";
import { systemSlotsKey, SYSTEM_SLOTS_PREFIX } from "../lib/system_slots";
import { SYSTEM_HEALTH_PREFIX } from "../lib/health";
import { ME_KEY, type Me } from "../lib/auth";
import { componentCommandsKey } from "../lib/commands";
import { EVENTS_KEY } from "../lib/events";
import { ENDPOINTS_KEY } from "../lib/endpoints";
import { DRIVERS_KEY } from "../lib/drivers";
import { uuidFor } from "../lib/testids";
import { seedSeverityLevels } from "../lib/testseverity";
import { answerConfirms } from "../lib/confirm.testkit";

// ComponentDetail (#2423): the component page and the component blade render
// ONE body, so a blade shows every section, tab and action its page shows.
// The host decides only where the tab lives (the page's ?tab=, the blade's own
// signal), whether Edit answers ?edit=1, and where a Delete leaves to.

const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const CID = uuidFor("cd-c-bar");
const SID = uuidFor("cd-s-a");

const view: FleetView = {
  locations: [
    { id: uuidFor("cd-hq"), name: "hq", label: "Headquarters", location_type: "campus", parent: "", health: "healthy" },
    { id: uuidFor("cd-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", parent: uuidFor("cd-hq"), health: "healthy" },
  ],
  systems: [
    { id: SID, name: "boardroom", label: "Boardroom System", location: uuidFor("cd-room"), health: "degraded", dots: [{ component: CID, name: "videobar-1", health: "outage", primary: true, shared: false }] },
  ],
} as unknown as FleetView;

const iso = (secondsAgo: number) => new Date(Date.now() - secondsAgo * 1000).toISOString();

function seed(actions: string[] = ["update", "delete"]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  seedSeverityLevels(qc);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...COMPONENTS_KEY], [{ id: CID, name: "videobar-1", label: "Video Bar 1", location_id: uuidFor("cd-room"), product: "kestrel-vroom", actions }]);
  qc.setQueryData([...PRODUCTS_KEY], [{ id: uuidFor("cd-p"), name: "kestrel-vroom", label: "Kestrel VRoom", vendor: "kestrel", driver: "kestrel-http" }]);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemSlotsKey(SID)], [{ name: "conf-bar", label: "Conferencing Bar", assigned_to: ["videobar-1"] }]);
  qc.setQueryData([...componentSystemsKey(CID)], [{ component: "videobar-1", system: "boardroom", system_id: SID, primary: true, system_count: 1 }]);
  qc.setQueryData([...REACHABILITY_KEY(CID)], { component: "videobar-1", endpoints: [] });
  qc.setQueryData([...componentAlarmsReadKey(CID)], {
    alarms: [{ id: "al-1", component: "videobar-1", component_id: CID, severity: "high", message: "No route to host", raised_at: iso(9600), active: true, acknowledged: false, raised_by_rule: "icmp-unreachable" }],
  });
  qc.setQueryData([...effectivePropertiesKey(CID)], [{ property_type_name: "serial-number", label: "Serial Number", value: "SN-1183", is_set: true, from_contract: true }]);
  qc.setQueryData([...effectiveMetricsKey(CID)], [{ metric_type_name: "icmp-rtt-avg", label: "RTT avg", data_type: "float", value: 6.1, is_sampled: true, from_contract: false, required: false }]);
  qc.setQueryData([...componentCommandsKey(CID)], []);
  qc.setQueryData([...EVENTS_KEY(CID)], { component: "videobar-1", events: [] });
  qc.setQueryData([...ENDPOINTS_KEY], []);
  qc.setQueryData([...DRIVERS_KEY], []);
  return qc;
}

function mountPage(path = `/web/components/${CID}`, qc = seed()) {
  window.history.pushState({}, "", path);
  render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/components/:id" component={ComponentLeaf} />
        <Route path="/explore" component={() => <div data-testid="explore-page" />} />
      </Router>
    </QueryClientProvider>
  ));
  return qc;
}

function mountBlade(path = "/web/explore?tab=configure", qc = seed()) {
  window.history.pushState({}, "", path);
  let controller!: BladeController;
  render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route
          path="/explore"
          component={() => {
            controller = createBladeController();
            controller.push({ kind: "component", id: CID });
            return (
              <BladesContext.Provider value={controller}>
                <BladeStack controller={controller} registry={fleetRegistry} />
              </BladesContext.Provider>
            );
          }}
        />
      </Router>
    </QueryClientProvider>
  ));
  return { qc, controller: () => controller };
}

// The section ids a host renders for the body, in document order.
const sections = (root: HTMLElement) => [...root.querySelectorAll("[data-testid]")].map((e) => e.getAttribute("data-testid")!).filter((t) => !t.startsWith("vital-") && !t.startsWith("blade-"));

async function sectionsPerTab(root: () => HTMLElement) {
  const out: Record<string, string[]> = {};
  for (const tab of ["Overview", "Activity", "Configure"]) {
    fireEvent.click(within(root()).getByRole("tab", { name: tab }));
    await waitFor(() => expect(within(root()).getByRole("tab", { name: tab }).getAttribute("aria-selected")).toBe("true"));
    if (tab === "Configure") await within(root()).findByTestId("entity-form");
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
    const onPage = await sectionsPerTab(() => screen.getByTestId("component-detail"));
    cleanup();
    mountBlade();
    const blade = await screen.findByRole("dialog");
    await within(blade).findByTestId("component-detail");
    const inBlade = await sectionsPerTab(() => within(blade).getByTestId("component-detail"));
    expect(inBlade).toEqual(onPage);
    expect(onPage.Overview).toEqual(expect.arrayContaining(["leaf-header", "tab-rail", "leaf-alarms", "leaf-identity", "leaf-memberships", "leaf-vitals", "leaf-collection"]));
    expect(onPage.Activity).toContain("activity-tab");
    expect(onPage.Configure).toEqual(expect.arrayContaining(["configure-face", "entity-form"]));
  });

  it("the fleet blade is the wide variant", async () => {
    mountBlade();
    const blade = await screen.findByRole("dialog");
    expect(blade.getAttribute("data-blade-width")).toBe("wide");
  });

  it("each Why row carries the rule, the actions and Troubleshoot in both hosts", async () => {
    mountPage();
    const why = await screen.findByTestId("leaf-alarms");
    expect(within(why).getByText(/by rule/).textContent).toContain("icmp-unreachable");
    expect(within(why).getByRole("button", { name: "Troubleshoot No route to host with omnibot" })).toBeTruthy();
    expect(within(within(why).getByTestId("alarm-controls")).getAllByRole("button").length).toBeGreaterThan(1);
  });
});

describe("a failed read in a blade", () => {
  it("says the read failed and offers Retry, never that the component was deleted", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({ title: "Bad Gateway", status: 502, detail: "upstream down" }), { status: 502, headers: { "Content-Type": "application/problem+json" } }));
    const qc = seed();
    qc.removeQueries({ queryKey: [...COMPONENTS_KEY] });
    mountBlade("/web/explore", qc);
    const blade = await screen.findByRole("dialog");
    expect(await within(blade).findByRole("button", { name: "Retry" })).toBeTruthy();
    expect(within(blade).queryByText(/No component answers/)).toBeNull();
  });
});

describe("the tab is the host's", () => {
  it("switching tabs in a blade leaves the page's address untouched", async () => {
    mountBlade("/web/explore?node=x");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Activity" }));
    expect(within(blade).getByTestId("activity-tab")).toBeTruthy();
    expect(window.location.search).toBe("?node=x");
  });

  it("a page's ?tab= is not the blade's: the blade opens on Overview", async () => {
    mountBlade("/web/explore?tab=configure");
    const blade = await screen.findByRole("dialog");
    expect((await within(blade).findByRole("tab", { name: "Overview" })).getAttribute("aria-selected")).toBe("true");
  });

  it("on a page, a tab writes ?tab= and an old address still lands", async () => {
    mountPage(`/web/components/${CID}?tab=events`);
    expect((await screen.findByRole("tab", { name: "Activity" })).getAttribute("aria-selected")).toBe("true");
    fireEvent.click(screen.getByRole("tab", { name: "Configure" }));
    await waitFor(() => expect(window.location.search).toBe("?tab=configure"));
  });
});

describe("one edit model", () => {
  it("edits from the blade's Configure, and the leave guard asks before a tab switch drops drafts", async () => {
    const confirm = answerConfirms(false);
    mountBlade("/web/explore");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    fireEvent.click(await within(blade).findByRole("button", { name: "Edit" }));
    const name = await within(blade).findByDisplayValue("videobar-1");
    fireEvent.input(name, { target: { value: "videobar-2" } });
    fireEvent.click(within(blade).getByRole("tab", { name: "Overview" }));
    expect(confirm).toHaveBeenCalled();
    expect(within(blade).getByRole("tab", { name: "Configure" }).getAttribute("aria-selected")).toBe("true");
    expect(window.location.search).toBe("");
  });

  it("the page's ?edit=1 lands on Configure, already editing", async () => {
    mountPage(`/web/components/${CID}?edit=1`);
    expect(await screen.findByDisplayValue("videobar-1")).toBeTruthy();
  });
});

describe("Delete, by the host's way out", () => {
  it("in a blade, refreshes the slots and health reads and closes the stack", async () => {
    answerConfirms(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(null, { status: 204 }));
    const r = mountBlade("/web/explore");
    const spy = vi.spyOn(r.qc, "invalidateQueries");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    fireEvent.click(await within(blade).findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(r.controller().stack()).toEqual([]));
    const keys = spy.mock.calls.map((c) => (c[0] as { queryKey: readonly unknown[] }).queryKey);
    expect(keys).toContainEqual([...SYSTEM_SLOTS_PREFIX]);
    expect(keys).toContainEqual([...SYSTEM_HEALTH_PREFIX]);
  });

  it("a refused delete keeps the blade open and says why", async () => {
    answerConfirms(true);
    const detail = "component is still referenced by another record, for example a system slot it staffs";
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify({ title: "Conflict", status: 409, detail }), { status: 409, headers: { "Content-Type": "application/problem+json" } }),
    );
    const r = mountBlade("/web/explore");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    fireEvent.click(await within(blade).findByRole("button", { name: "Delete" }));
    expect((await within(blade).findByRole("alert")).textContent).toContain("system slot it staffs");
    expect(r.controller().stack().length).toBe(1);
  });

  it("on the page, leaves for Explore", async () => {
    answerConfirms(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(null, { status: 204 }));
    mountPage(`/web/components/${CID}?tab=configure`);
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    expect(await screen.findByTestId("explore-page")).toBeTruthy();
  });
});

describe("a nested open stacks on the ambient stack", () => {
  it("a system it serves opens over the component's blade", async () => {
    const r = mountBlade("/web/explore");
    const blade = await screen.findByRole("dialog");
    const serves = await within(blade).findByTestId("leaf-memberships");
    fireEvent.click(within(serves).getByRole("button", { name: "Boardroom System" }));
    await waitFor(() => expect(r.controller().stack().map((x) => x.kind)).toEqual(["component", "system"]));
  });
});
