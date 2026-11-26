import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
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
import { clickDelete } from "./configure.testkit";
import { answerConfirms } from "../lib/confirm.testkit";
import { fmtTime } from "../lib/format";
import { seedSystemHealth } from "../lib/health.testkit";

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
  seedSystemHealth(qc, SID, health);
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

  // One rule raises one alarm per endpoint: a display down on both its
  // endpoints reads two Reachability lost rows, each naming its endpoint.
  it("names each alarm's endpoint, so two alarms of one rule on one component read apart", async () => {
    const qc = seed();
    const a = health.slots![0].alarms![0];
    seedSystemHealth(qc, SID, {
      ...health,
      slots: [{ ...health.slots![0], alarms: [
        { ...a, id: "al-snmp", message: "Reachability lost", instance: "snmp" },
        { ...a, id: "al-http", message: "Reachability lost", instance: "http" },
      ] }],
    });
    mountPage(undefined, qc);
    const why = await screen.findByTestId("alarm-strip");
    await waitFor(() => expect(within(why).getAllByTestId("alarm-instance")).toHaveLength(2));
    expect(within(why).getAllByTestId("alarm-instance").map((e) => e.textContent).sort()).toEqual(["on http", "on snmp"]);
    expect(why.textContent).toContain("Reachability lost on snmp");
  });

  // Snooze is offered on a system's alarm as on a component's (#2448): Snooze
  // while it is not snoozed, the chip and Unsnooze once it is.
  it("an unsnoozed alarm offers Snooze, a snoozed one its chip and Unsnooze, page and blade", async () => {
    const snoozed = (until?: string) => {
      const qc = seed();
      seedSystemHealth(qc, SID, {
        ...health,
        slots: [{ ...health.slots![0], alarms: [{ ...health.slots![0].alarms![0], ...(until ? { snoozed_until: until, snoozed_by: "alice" } : {}) }] }],
      });
      return qc;
    };
    mountPage(undefined, snoozed());
    let controls = within(await screen.findByTestId("alarm-strip")).getByTestId("alarm-controls");
    expect(within(controls).getByRole("button", { name: /^Snooze/ })).toBeTruthy();
    expect(within(controls).queryByRole("button", { name: /^Unsnooze/ })).toBeNull();
    cleanup();
    const until = new Date(Date.now() + 3_600_000).toISOString();
    mountPage(undefined, snoozed(until));
    controls = within(await screen.findByTestId("alarm-strip")).getByTestId("alarm-controls");
    expect(within(controls).getByRole("button", { name: /^Unsnooze/ })).toBeTruthy();
    expect(within(controls).queryByRole("button", { name: /^Snooze/ })).toBeNull();
    cleanup();
    mountBlade(undefined, snoozed());
    const blade = await screen.findByRole("dialog");
    controls = within(await within(blade).findByTestId("alarm-strip")).getByTestId("alarm-controls");
    expect(within(controls).getByRole("button", { name: /^Snooze/ })).toBeTruthy();
    expect(within(controls).getByRole("button", { name: /acknowledge/i })).toBeTruthy();
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

describe("a system with no components (#2464)", () => {
  const blank = () => {
    const qc = seed();
    qc.setQueryData([...FLEET_VIEW_KEY], { ...view, systems: [{ ...view.systems![0], dots: [] }] });
    seedSystemHealth(qc, SID, { ...health, slots: [], alarms: [] });
    qc.setQueryData([...systemSlotsKey(SID)], []);
    qc.setQueryData([...SYSTEMS_KEY], [{ id: SID, name: "boardroom", label: "Boardroom", location_id: uuidFor("sd-room"), actions: ["update"] }]);
    return qc;
  };
  it("the page and the blade teach and offer Add component and Start from a standard", async () => {
    mountPage(undefined, blank());
    const page = await screen.findByTestId("system-empty");
    expect(within(page).getByRole("button", { name: "Add component" })).toBeTruthy();
    expect(within(page).getByRole("button", { name: "Start from a standard" })).toBeTruthy();
    cleanup();
    mountBlade(undefined, blank());
    const blade = await screen.findByRole("dialog");
    expect(await within(blade).findByTestId("system-empty")).toBeTruthy();
  });
  it("Start from a standard lands on Configure", async () => {
    mountPage(undefined, blank());
    fireEvent.click(await screen.findByRole("button", { name: "Start from a standard" }));
    expect(await screen.findByTestId("configure-face")).toBeTruthy();
  });
  it("a system with members shows no empty state", async () => {
    mountPage(undefined, seed());
    await screen.findByTestId("system-detail");
    expect(screen.queryByTestId("system-empty")).toBeNull();
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

  it("a blade opens on the page's range, and a range chosen in it is its own: the page's address keeps its range", async () => {
    mountBlade("/web/explore?range=1h");
    const blade = await screen.findByRole("dialog");
    const trigger = await within(blade).findByTestId("time-range-trigger");
    // The blade opens on the page's ?range=, as a component's blade does.
    expect(trigger.textContent).toContain("Last 1 hour");
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByTestId("time-range-preset-7d"));
    await waitFor(() => expect(within(blade).getByTestId("time-range-trigger").textContent).toContain("Last 7 days"));
    expect(window.location.search).toBe("?range=1h");
  });

  it("edits the system from the blade's Configure, and a page ?edit=location does not reach it", async () => {
    mountBlade("/web/explore?edit=location");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    const face = await within(blade).findByTestId("configure-face");
    expect(within(blade).queryByDisplayValue("boardroom-a")).toBeNull();
    fireEvent.click(await within(await within(blade).findByTestId("tab-rail")).findByRole("button", { name: "Edit" }));
    expect(await within(face).findByDisplayValue("boardroom")).toBeTruthy();
    expect(window.location.search).toBe("?edit=location");
  });

  // A sole system's location has no page of its own (its address lands on
  // the system), so its section here carries the location's own Delete, on
  // location:delete (#2463): without it a place holding one system could not
  // be deleted from the console at all.
  it("a sole system's location section carries the location's own Delete", async () => {
    const qc = seed();
    qc.setQueryData([...LOCATIONS_KEY], [{ id: uuidFor("sd-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", actions: ["update", "delete"] }]);
    const asked = answerConfirms(false);
    mountPage(`/web/systems/${SID}?tab=configure`, qc);
    const face = await screen.findByTestId("configure-face-location");
    await clickDelete(face);
    await waitFor(() => expect(asked).toHaveBeenCalledWith(expect.stringContaining('Delete location "boardroom-a"?')));
  });

  it("the location section offers no Delete without location:delete", async () => {
    mountPage(`/web/systems/${SID}?tab=configure`);
    const face = await screen.findByTestId("configure-face-location");
    await within(face).findByRole("button", { name: "Edit" });
    expect(within(face).queryByRole("button", { name: "More actions" })).toBeNull();
  });

  it("closing the blade with unsaved drafts asks first", async () => {
    const asked = answerConfirms(false);
    const r = mountBlade();
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    const face = await within(blade).findByTestId("configure-face");
    fireEvent.click(await within(await within(blade).findByTestId("tab-rail")).findByRole("button", { name: "Edit" }));
    fireEvent.input(await within(face).findByDisplayValue("boardroom"), { target: { value: "boardroom-2" } });
    fireEvent.click(within(blade).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(asked).toHaveBeenCalled());
    expect(r.controller().stack().length).toBe(1);
  });

  it("deletes from the blade's Configure and closes the stack", async () => {
    answerConfirms(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(null, { status: 204 }));
    const r = mountBlade();
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    await clickDelete(blade);
    await waitFor(() => expect(r.controller().stack()).toEqual([]));
  });
});

// #2723: the Activity tab's history moves with the 5 s health poll that feeds
// it, not once a minute, so right after a recovery its record catches up with
// the header within one poll and nobody has to refresh.
describe("the Activity tab's history keeps time with the poll", () => {
  afterEach(() => vi.useRealTimers());

  it("a recovery's healthy time reaches Uptime within one 5 s poll", async () => {
    const recovered = Date.parse("2026-08-20T17:00:00Z");
    vi.useFakeTimers({ now: recovered, shouldAdvanceTime: true });
    const qc = seed();
    seedSystemHealth(qc, SID, {
      ...health,
      health: "healthy",
      slots: [],
      transitions: [
        { ts: new Date(recovered - 60_000).toISOString(), health: "healthy" },
        { ts: new Date(recovered - 30_000).toISOString(), health: "degraded" },
        { ts: new Date(recovered).toISOString(), health: "healthy" },
      ],
    });
    mountPage(`/web/systems/${SID}`, qc);
    fireEvent.click(await screen.findByRole("tab", { name: "Activity" }));
    const kpi = await screen.findByTestId("uptime-kpi");
    // Healthy 30 s of the 60 s recorded so far.
    await waitFor(() => expect(kpi.textContent).toContain("50.0"));
    vi.advanceTimersByTime(5_000);
    // 35 s healthy of 65 s: the history took the poll's next now.
    await waitFor(() => expect(kpi.textContent).toContain("53.8"));
  });
});

// #2741: the Activity tab follows the page's one range, as Overview and the
// Timeline do. With no range in the address it opens on the last 30 days (its
// status-page default); a range once picked governs every tab.
describe("the Activity tab follows the page's range (#2741)", () => {
  const NOW = Date.parse("2026-08-20T16:00:00Z");
  const iso = (ms: number) => new Date(ms).toISOString();
  const HOUR = 3600_000;
  // Healthy when the hour opened, in outage for its last half.
  const hourly = {
    ...health,
    health: "outage",
    carried_in: "healthy",
    first_recorded_at: "2026-07-01T00:00:00Z",
    transitions: [{ ts: iso(NOW - HOUR / 2), health: "outage" }],
  } as unknown as FleetHealth;

  // Every health read goes through fetch, answered with the report above.
  const stubHealth = (body: unknown = hourly) => {
    const reads: URL[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const req = input instanceof Request ? input : new Request(String(input));
        const url = new URL(req.url);
        if (url.pathname.endsWith(`/systems/${SID}/health`)) {
          reads.push(url);
          return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
        }
        const out = req.method === "POST" ? { results: [], cursor: "" } : [];
        return new Response(JSON.stringify(out), { status: 200, headers: { "content-type": "application/json" } });
      }),
    );
    return reads;
  };
  // A cache with no health in it, so the page's own read is the one drawn.
  const unseeded = () => {
    const qc = seed();
    qc.removeQueries({ queryKey: [...systemHealthKey(SID)] });
    return qc;
  };
  const since = (u: URL) => Date.parse(u.searchParams.get("since") ?? "");

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(NOW));
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("with range=1h, the health read carries that hour and Uptime is that hour's, labelled for it", async () => {
    const reads = stubHealth();
    mountPage(`/web/systems/${SID}?tab=activity&range=1h`, unseeded());
    await waitFor(() => expect(reads.length).toBeGreaterThan(0));
    expect(since(reads[0])).toBe(NOW - HOUR);
    expect(reads[0].searchParams.get("until")).toBeNull();
    const kpi = await screen.findByTestId("uptime-kpi");
    await waitFor(() => expect(kpi.textContent).toContain("of the last hour"));
    expect(kpi.textContent).toContain("50.0%");
    expect(screen.getByTestId("time-range-trigger").textContent).toContain("Last 1 hour");
  });

  it("with no range given, Activity reads the last 30 days and the Timeline the last 24 hours", async () => {
    const reads = stubHealth();
    mountPage(`/web/systems/${SID}?tab=activity`, unseeded());
    await waitFor(() => expect(reads.length).toBeGreaterThan(0));
    expect(since(reads[0])).toBe(NOW - 30 * 24 * HOUR);
    const kpi = await screen.findByTestId("uptime-kpi");
    await waitFor(() => expect(kpi.textContent).toContain("of the last 30 days"));
    expect(screen.getByTestId("time-range-trigger").textContent).toContain("Last 30 days");
    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    await waitFor(() => expect(screen.getByTestId("time-range-trigger").textContent).toContain("Last 24 hours"));
    expect(window.location.search).not.toContain("range=");
  });

  it("the history bar starts at the range's start, the carried-in health before the first edge", async () => {
    stubHealth();
    mountPage(`/web/systems/${SID}?tab=activity&range=1h`, unseeded());
    const strip = await screen.findByTestId("health-history-full");
    await waitFor(() => expect(within(strip).getByTestId("strip-start").textContent).toBe(fmtTime(iso(NOW - HOUR))));
    const segs = [...strip.querySelectorAll("[data-segment]")].map((el) => el.getAttribute("data-segment")!.split(":")[0]);
    expect(segs).toEqual(["healthy", "outage"]);
  });

  it("the picker shows on Activity, and a pick there moves the Timeline's range too", async () => {
    stubHealth();
    mountPage(`/web/systems/${SID}?tab=activity`, unseeded());
    fireEvent.click(await screen.findByTestId("time-range-trigger"));
    fireEvent.click(await screen.findByTestId("time-range-preset-7d"));
    await waitFor(() => expect(window.location.search).toContain("range=7d"));
    await waitFor(() => expect(screen.getByTestId("uptime-kpi").textContent).toContain("of the last 7 days"));
    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    await waitFor(() => expect(screen.getByTestId("time-range-trigger").textContent).toContain("Last 7 days"));
  });

  // The default alarm read holds the active alarms and those cleared in the
  // last 30 days; a custom range reads its own, so an old outage keeps the
  // alarms that explain it.
  it("a custom range reads its health and its alarms over that range", async () => {
    const reads: URL[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const req = input instanceof Request ? input : new Request(String(input));
        const url = new URL(req.url);
        reads.push(url);
        const body = url.pathname.endsWith("/health") ? old : url.pathname.endsWith("/alarms") ? { alarms: [] } : [];
        return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      }),
    );
    const from = NOW - 60 * 24 * HOUR;
    const to = from + 2 * HOUR;
    // Healthy when the range opened, out for its second hour.
    const old = { ...hourly, first_recorded_at: "2026-01-01T00:00:00Z", transitions: [{ ts: iso(from + HOUR), health: "outage" }] };
    const qc = unseeded();
    qc.removeQueries({ queryKey: [...systemAlarmsKey(SID)] });
    mountPage(`/web/systems/${SID}?tab=activity&from=${iso(from)}&to=${iso(to)}`, qc);
    await waitFor(() => expect(reads.some((u) => u.pathname.endsWith(`/systems/${SID}/alarms`))).toBe(true));
    const alarmsRead = reads.find((u) => u.pathname.endsWith(`/systems/${SID}/alarms`))!;
    expect(alarmsRead.searchParams.get("since")).toBe(iso(from));
    expect(alarmsRead.searchParams.get("until")).toBe(iso(to));
    const healthRead = reads.find((u) => u.pathname.endsWith(`/systems/${SID}/health`))!;
    expect(healthRead.searchParams.get("since")).toBe(iso(from));
    expect(healthRead.searchParams.get("until")).toBe(iso(to));
    const kpi = await screen.findByTestId("uptime-kpi");
    await waitFor(() => expect(kpi.textContent).toContain(`${fmtTime(iso(from))} → ${fmtTime(iso(to))}`));
    expect(kpi.textContent).toContain("50.0%");
    // The bar ends at the range's end, not now.
    expect(within(screen.getByTestId("health-history-full")).getByTestId("strip-end").textContent).toBe(fmtTime(iso(to)));
  });

  // The address leaves the 24-hour default out, so on this page a pick of 24
  // hours has to say so: otherwise Activity would fall back to its 30 days.
  it("a pick of the last 24 hours on Activity holds there", async () => {
    stubHealth();
    mountPage(`/web/systems/${SID}?tab=activity`, unseeded());
    fireEvent.click(await screen.findByTestId("time-range-trigger"));
    fireEvent.click(await screen.findByTestId("time-range-preset-24h"));
    await waitFor(() => expect(window.location.search).toContain("range=24h"));
    await waitFor(() => expect(screen.getByTestId("uptime-kpi").textContent).toContain("of the last 24 hours"));
    expect(screen.getByTestId("time-range-trigger").textContent).toContain("Last 24 hours");
  });
});
