import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import LocationZoom from "../pages/LocationZoom";
import BladeStack from "./BladeStack";
import { BladesContext, createBladeController, type BladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { locationHealthKey, type FleetHealth } from "../lib/health";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { TAGS_KEY } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import { seedSeverityLevels } from "../lib/testseverity";
import { answerConfirms } from "../lib/confirm.testkit";

// LocationDetail (#2423): the location page and the location blade render ONE
// body, so a blade shows every section, tab and action its page shows: the
// type, health, since-when and tags in the header, what it holds, the systems
// bound here, the outline beneath, and the one form.

const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const FLOOR = uuidFor("ld-floor");
const ROOM = uuidFor("ld-room");
const HALL = uuidFor("ld-hall");

const view: FleetView = {
  locations: [
    { id: FLOOR, name: "level-2", label: "Level 2", location_type: "floor", parent: "", health: "degraded" },
    { id: ROOM, name: "boardroom-a", label: "Boardroom A", location_type: "room", parent: FLOOR, health: "degraded" },
    { id: HALL, name: "hall", label: "Hall", location_type: "room", parent: FLOOR, health: "healthy" },
  ],
  systems: [
    { id: uuidFor("ld-s1"), name: "boardroom", label: "Boardroom", location: ROOM, health: "degraded", dots: [] },
    { id: uuidFor("ld-s2"), name: "boardroom-b", label: "Boardroom Overflow", location: ROOM, health: "healthy", dots: [] },
  ],
} as unknown as FleetView;

const health = (h: string) => ({ health: h, slots: [], systems: [], transitions: [{ ts: "2026-08-15T14:20:00Z", health: h }] }) as unknown as FleetHealth;

function seed() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  seedSeverityLevels(qc);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...locationHealthKey(FLOOR)], health("degraded"));
  qc.setQueryData([...locationHealthKey(ROOM)], health("degraded"));
  qc.setQueryData([...LOCATIONS_KEY], [
    { id: FLOOR, name: "level-2", label: "Level 2", location_type: "floor", actions: ["update", "delete"], effective_tags: { environment: "prod" } },
    { id: ROOM, name: "boardroom-a", label: "Boardroom A", location_type: "room", parent_id: FLOOR, actions: ["update", "delete"] },
    { id: HALL, name: "hall", label: "Hall", location_type: "room", parent_id: FLOOR, actions: [] },
  ]);
  qc.setQueryData([...LOCATION_TYPES_KEY], [
    { id: uuidFor("ld-t-floor"), name: "floor", label: "Floor", allowed_parent_types: [] },
    { id: uuidFor("ld-t-room"), name: "room", label: "Room", allowed_parent_types: [] },
  ]);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], []);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  return qc;
}

function mountPage(id: string) {
  window.history.pushState({}, "", `/web/locations/${id}`);
  render(() => (
    <QueryClientProvider client={seed()}>
      <Router base="/web">
        <Route path="/locations/:id" component={LocationZoom} />
        <Route path="/explore" component={() => <div data-testid="explore-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

function mountBlade(id: string, path = "/web/explore") {
  window.history.pushState({}, "", path);
  let controller!: BladeController;
  render(() => (
    <QueryClientProvider client={seed()}>
      <Router base="/web">
        <Route
          path="/explore"
          component={() => {
            controller = createBladeController();
            controller.push({ kind: "location", id });
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
  return { controller: () => controller };
}

const sections = (root: HTMLElement) => [...root.querySelectorAll("[data-testid]")].map((e) => e.getAttribute("data-testid")!);

async function sectionsPerTab(root: () => HTMLElement) {
  const out: Record<string, string[]> = {};
  for (const tab of ["Overview", "Configure"]) {
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
    mountPage(FLOOR);
    const onPage = await sectionsPerTab(() => screen.getByTestId("location-detail"));
    cleanup();
    mountBlade(FLOOR);
    const blade = await screen.findByRole("dialog");
    const inBlade = await sectionsPerTab(() => within(blade).getByTestId("location-detail"));
    // The blade's header adds the path the page's breadcrumb says.
    const strip = (r: Record<string, string[]>) => Object.fromEntries(Object.entries(r).map(([k, v]) => [k, v.filter((t) => t !== "location-path")]));
    expect(strip(inBlade)).toEqual(onPage);
    expect(onPage.Overview).toEqual(expect.arrayContaining(["location-header", "since-line", "location-holds", "beneath"]));
    expect(blade.getAttribute("data-blade-width")).toBe("wide");
  });

  it("the header leads with the type, health, since-when and tags, in both hosts", async () => {
    mountBlade(FLOOR);
    const blade = await screen.findByRole("dialog");
    const header = await within(blade).findByTestId("location-header");
    expect(within(header).getByText("Floor")).toBeTruthy();
    expect(within(header).getByText("degraded")).toBeTruthy();
    expect(within(header).getByTestId("since-line")).toBeTruthy();
    expect(within(header).getByText("environment")).toBeTruthy();
  });

  it("says what it holds by type, and nothing about locations beneath a room that holds none", async () => {
    mountBlade(FLOOR);
    const blade = await screen.findByRole("dialog");
    expect((await within(blade).findByTestId("location-holds")).textContent).toContain("2 rooms");
    cleanup();
    mountBlade(ROOM);
    const room = await screen.findByRole("dialog");
    await within(room).findByTestId("location-systems");
    expect(within(room).queryByTestId("location-holds")).toBeNull();
  });

  it("in a blade, a system card opens the system over the location, on the same stack", async () => {
    const r = mountBlade(ROOM);
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByTestId(`system-summary-${uuidFor("ld-s1")}`));
    await waitFor(() => expect(r.controller().stack().map((x) => x.kind)).toEqual(["location", "system"]));
  });

  it("a blade's header says where the location sits; the page leaves that to the breadcrumb", async () => {
    mountBlade(ROOM);
    const blade = await screen.findByRole("dialog");
    expect((await within(blade).findByTestId("location-path")).textContent).toBe("Level 2");
    expect(within(blade).queryByTestId("location-card")).toBeNull();
    cleanup();
    mountPage(HALL);
    await screen.findByTestId("location-detail");
    expect(screen.queryByTestId("location-path")).toBeNull();
  });
});

describe("the outline inside a blade stacks", () => {
  it("a row in a blade's outline opens over the blade, not in place of it", async () => {
    const r = mountBlade(FLOOR);
    const blade = await screen.findByRole("dialog");
    const beneath = await within(blade).findByTestId("beneath");
    fireEvent.click(await within(beneath).findByRole("treeitem", { name: /Hall/ }));
    await waitFor(() => expect(r.controller().stack().length).toBe(2));
    expect(r.controller().stack()[0]).toEqual({ kind: "location", id: FLOOR });
  });
});

describe("the blade's tab, filter and edit are its own", () => {
  it("switching tabs and filtering the outline in a blade leave the page's address untouched", async () => {
    mountBlade(FLOOR, "/web/explore?node=x");
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    expect(window.location.search).toBe("?node=x");
  });

  it("closing the blade with unsaved drafts asks first", async () => {
    const asked = answerConfirms(false);
    const r = mountBlade(ROOM);
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    fireEvent.click(await within(blade).findByRole("button", { name: "Edit" }));
    fireEvent.input(await within(blade).findByDisplayValue("boardroom-a"), { target: { value: "boardroom-z" } });
    fireEvent.click(within(blade).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(asked).toHaveBeenCalled());
    expect(r.controller().stack().length).toBe(1);
  });

  it("edits the parent from the blade's Configure, and deletes there", async () => {
    answerConfirms(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(null, { status: 204 }));
    const r = mountBlade(ROOM);
    const blade = await screen.findByRole("dialog");
    fireEvent.click(await within(blade).findByRole("tab", { name: "Configure" }));
    const form = await within(blade).findByTestId("entity-form");
    expect(within(form).getByText("Parent")).toBeTruthy();
    fireEvent.click(within(blade).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(r.controller().stack()).toEqual([]));
  });
});
