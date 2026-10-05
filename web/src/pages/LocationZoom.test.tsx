import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, within, cleanup, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Locations from "./Locations";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { locationHealthKey, systemHealthKey, type FleetHealth } from "../lib/health";
import { LOCATION_TYPES_KEY, type LocationType } from "../lib/location_types";
import { LOCATIONS_KEY } from "../lib/locations";
import { SYSTEMS_KEY } from "../lib/systems";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY } from "../lib/tags";
import { COMPONENTS_KEY } from "../lib/components";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { COMPONENT_TYPES_KEY } from "../lib/component_types";
import { uuidFor } from "../lib/testids";

// A place's detail view (#872): systems are the unit, places are folders. A
// place holding one system lands on that system; a folder (no system, or a
// place shared by several) shows its own card with its tabs: a brief card per
// system it holds, then what is beneath it as the outline rooted here.

const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

const loc = (handle: string, name: string, label: string, type: string, parent: string, verdict: string) => ({
  id: uuidFor(handle),
  name,
  label,
  location_type: type,
  location_type_id: uuidFor(`lzt-${type}`),
  parent: parent ? uuidFor(parent) : "",
  verdict,
});

const view: FleetView = {
  locations: [
    loc("lz-hq", "hq", "Headquarters", "campus", "", "degraded"),
    loc("lz-b1", "west", "West Building", "building", "lz-hq", "degraded"),
    loc("lz-yard", "yard", "The Yard", "area", "lz-hq", "healthy"),
    loc("lz-room", "boardroom-a", "Boardroom A", "room", "lz-b1", "degraded"),
    // A leaf directly under hq holding nothing: the anchor's own hole.
    loc("lz-shed", "shed", "The Shed", "room", "lz-hq", "healthy"),
  ],
  systems: [
    // Attached to hq itself: the placed-here band.
    {
      id: uuidFor("lz-s-lobby"),
      name: "lobby-av",
      label: "Lobby AV",
      location: uuidFor("lz-hq"),
      verdict: "incomplete",
      dots: [{ component: uuidFor("lz-c-sign"), name: "display-1", verdict: "incomplete", primary: true, shared: false }],
    },
    // A second system at hq, so hq is a place shared by two and keeps its
    // own view.
    {
      id: uuidFor("lz-s-sign"),
      name: "lobby-signage",
      label: "Lobby Signage",
      location: uuidFor("lz-hq"),
      verdict: "healthy",
      dots: [],
    },
    // Deep under west, alone in its room: that room lands on it.
    {
      id: uuidFor("lz-s-board"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("lz-room"),
      verdict: "degraded",
      dots: [{ component: uuidFor("lz-c-bar"), name: "videobar-1", verdict: "degraded", primary: true, shared: false }],
    },
    // At the yard, an AREA type: a child band regardless of type.
    {
      id: uuidFor("lz-s-yard"),
      name: "yard-av",
      label: "Yard AV",
      location: uuidFor("lz-yard"),
      verdict: "healthy",
      dots: [{ component: uuidFor("lz-c-horn"), name: "speaker-1", verdict: "healthy", primary: true, shared: false }],
    },
  ],
} as unknown as FleetView;

// The lobby system's health: an unstaffed role, which must read incomplete on
// the card, in the server's own words, never outage.
const lobbyHealth: FleetHealth = {
  verdict: "incomplete",
  roles: [
    {
      name: "signage",
      label: "Signage",
      impact: "outage",
      quorum: 2,
      satisfying: 1,
      short: 1,
      spare: 0,
      impaired: true,
      active: true,
      assigned_to: ["display-1"],
      down: [],
      alarms: [],
    },
  ],
  systems: [],
  transitions: [],
} as unknown as FleetHealth;

const types: LocationType[] = [
  { id: uuidFor("lzt-campus"), name: "campus", label: "Campus", icon: "landmark", official: true, forked: false, allowed_parent_types: ["root"] },
  { id: uuidFor("lzt-building"), name: "building", label: "Building", icon: "building", official: true, forked: false, allowed_parent_types: ["root", "campus"] },
  { id: uuidFor("lzt-area"), name: "area", label: "Area", icon: "map-pin", official: false, forked: false, allowed_parent_types: [] },
  { id: uuidFor("lzt-room"), name: "room", label: "Room", icon: "door-open", official: true, forked: false, allowed_parent_types: ["building", "floor"] },
] as unknown as LocationType[];

// lateTypes starts the location-type catalog EMPTY, so a test can deliver it
// after the editor is open and reproduce the order a deep link actually hits:
// the row is in hand well before the catalog answers (#782).
function mount(path = `/web/locations/${uuidFor("lz-hq")}`, lateTypes = false) {
  localStorage.removeItem("fleet-sumopen");
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATION_TYPES_KEY], lateTypes ? [] : types);
  // The configure face self-fetches its row and parent options from this
  // list; a test that strips it renders the face as a skeleton (#800-1).
  qc.setQueryData([...LOCATIONS_KEY], [
    { id: uuidFor("lz-hq"), name: "hq", label: "Headquarters", location_type: "campus", parent_id: null, actions: ["update", "delete", "move", "rename"] },
    { id: uuidFor("lz-b1"), name: "west", label: "West Building", location_type: "building", parent_id: uuidFor("lz-hq"), actions: ["update", "delete", "move", "rename"] },
    { id: uuidFor("lz-hq2"), name: "lab", label: "Lab", location_type: "campus", parent_id: null, actions: ["update"] },
  ]);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], []);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...COMPONENT_TYPES_KEY], []);
  qc.setQueryData([...systemHealthKey(uuidFor("lz-s-sign"))], { verdict: "healthy", roles: [], systems: [], transitions: [] } as unknown as FleetHealth);
  qc.setQueryData([...systemHealthKey(uuidFor("lz-s-lobby"))], lobbyHealth);
  qc.setQueryData([...locationHealthKey(uuidFor("lz-hq"))], {
    verdict: "degraded",
    roles: [],
    systems: [],
    transitions: [
      { ts: "2026-08-01T09:00:00Z", verdict: "healthy" },
      { ts: "2026-08-15T14:20:00Z", verdict: "degraded" },
    ],
  } as unknown as FleetHealth);
  window.history.pushState({}, "", path);
  const r = render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/locations/:id" component={Locations} />
        <Route path="/explore" component={() => <div data-testid="fleet-page" />} />
        <Route path="/systems/:id" component={() => <div data-testid="system-page" />} />
        <Route path="/locations/create" component={() => <div data-testid="create-page" />} />
      </Router>
    </QueryClientProvider>
  ));
  return Object.assign(r, { qc });
}

afterEach(cleanup);

describe("where a place lands (#872)", () => {
  it("lands a place holding one system on that system, query kept", async () => {
    mount(`/web/locations/${uuidFor("lz-room")}?chips=x`);
    expect(await screen.findByTestId("system-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/systems/${uuidFor("lz-s-board")}`);
    expect(window.location.search).toBe("?chips=x");
  });

  it("stays on the place when the address asks to configure the place itself", async () => {
    mount(`/web/locations/${uuidFor("lz-room")}?edit=1`);
    expect(await screen.findByTestId("configure-face")).toBeTruthy();
    expect(screen.queryByTestId("system-page")).toBeNull();
  });

  it("a name-shaped address resolves to the uuid, keeping the param (#759's rule)", async () => {
    mount(`/web/locations/west`);
    await waitFor(() => expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-b1")}`));
    expect(window.location.search).toBe("");
    expect(screen.getByTestId("place-subject")).toBeTruthy();
  });
});

describe("a folder place (#872)", () => {
  it("is titled by the place, names its type in the registry's words, and ends the path at its parent", () => {
    mount(`/web/locations/${uuidFor("lz-b1")}`);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("West Building");
    expect(within(screen.getByTestId("place-header")).getByTestId("place-type").textContent).toBe("Building");
    const crumbs = within(screen.getByTestId("breadcrumb"));
    expect(crumbs.getByText("Headquarters")).toBeTruthy();
    expect(crumbs.queryByText("West Building")).toBeNull();
  });

  it("gives each system it holds a brief card, with its verdict and its gap, opening the system", async () => {
    mount();
    const lobby = screen.getByTestId(`system-summary-${uuidFor("lz-s-lobby")}`);
    expect(within(lobby).getByText("Lobby AV")).toBeTruthy();
    expect(within(lobby).getByText("incomplete")).toBeTruthy();
    expect(within(lobby).getByText(/1 of 2 slots filled/)).toBeTruthy();
    expect(screen.getByTestId(`system-summary-${uuidFor("lz-s-sign")}`)).toBeTruthy();
    fireEvent.click(lobby);
    expect(await screen.findByTestId("system-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/systems/${uuidFor("lz-s-lobby")}`);
  });

  it("lists what is beneath it as the outline rooted here, counted by systems", () => {
    mount();
    const beneath = screen.getByTestId("beneath");
    const rows = within(beneath).getAllByRole("treeitem").map((r) => r.querySelector("[data-label]")?.textContent);
    // Sorted by name; West Building folds with Boardroom A (it holds only that
    // room), so that row sorts as West and names its deepest place.
    expect(rows).toEqual(["The Shed", "The Yard", "Boardroom A"]);
    expect(rows).toContain("The Yard");
    expect(rows).toContain("The Shed");
    expect(within(beneath).getByTestId("explore-counts").textContent).toMatch(/^4\s*systems/);
  });

  it("offers the create path under an empty folder", () => {
    mount(`/web/locations/${uuidFor("lz-shed")}`);
    const beneath = screen.getByTestId("beneath");
    expect(within(beneath).getByText("Nothing here yet.")).toBeTruthy();
    expect(within(beneath).getAllByRole("button", { name: /^New / }).length).toBeGreaterThan(0);
  });

  it("offers Retry when the fleet read fails", async () => {
    const r = mount();
    r.qc.getQueryCache().find({ queryKey: [...FLEET_VIEW_KEY] })?.setState({ status: "error", error: new Error("boom"), data: undefined });
    expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
  });
});

describe("the location configure tab (#800)", () => {
  it("offers Configure and renders identity, the parent mover, and tags", async () => {
    mount(`/web/locations/${uuidFor("lz-hq")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    expect(within(face).getByText("Identity")).toBeTruthy();
    expect(within(face).getByText("Placement")).toBeTruthy();
    expect(within(face).getAllByText("Tags").length).toBeGreaterThan(0);
    expect(within(face).getByText("hq")).toBeTruthy();
  });

  it("keeps the Overview the default facet", async () => {
    mount();
    const rail = await screen.findByTestId("tab-rail");
    expect(within(rail).getByRole("tab", { name: "Overview" }).getAttribute("aria-selected")).toBe("true");
    expect(within(rail).getByRole("tab", { name: "Configure" })).toBeTruthy();
    expect(screen.getByTestId("place-header")).toBeTruthy();
  });
});


// The mover behaviors the classic face carried, re-homed on Configure
// (#800 slice 3): narrowed candidates, uuid-valued options, self-exclusion,
// root only when currently root, and the refused move surfacing in place.
describe("the configure mover keeps the classic contract (#800)", () => {
  it("narrows to allowed_parent_types, excludes the subtree, uuid-valued", async () => {
    mount(`/web/locations/${uuidFor("lz-b1")}?tab=configure&edit=1`);
    const face = await screen.findByTestId("configure-face");
    const select = (await within(face).findByLabelText("Parent")) as HTMLSelectElement;
    const labels = Array.from(select.options).map((o) => o.textContent?.trim());
    // building allows [root, campus]: both campuses offered, never itself,
    // and no root option since it already has a parent.
    expect(labels).toContain("Headquarters");
    expect(labels).toContain("Lab");
    expect(labels.some((l) => l?.includes("West Building"))).toBe(false);
    expect(labels).not.toContain("Root (current)");
    // seeded from and valued by uuid (#627)
    expect(select.value).toBe(uuidFor("lz-hq"));
    fireEvent.change(select, { target: { value: uuidFor("lz-hq2") } });
    expect(select.value).toBe(uuidFor("lz-hq2"));
  });

  it("offers only the current-root placeholder when nothing matches the allowed set", async () => {
    mount(`/web/locations/${uuidFor("lz-hq")}?tab=configure&edit=1`);
    const face = await screen.findByTestId("configure-face");
    const select = (await within(face).findByLabelText("Parent")) as HTMLSelectElement;
    const labels = Array.from(select.options).map((o) => o.textContent?.trim());
    // campus allows [root] only: no location is of type root, so the only
    // option is the current-root placeholder.
    expect(labels).toEqual(["Root (current)"]);
  });

  it("sends the move by uuid on save and surfaces a refusal in place", async () => {
    const calls: { method: string; url: string; body: string }[] = [];
    const stub = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const body = req.method === "PATCH" || req.method === "POST" ? await req.clone().text() : "";
      calls.push({ method: req.method, url: req.url, body });
      if (req.url.includes(":move")) {
        return new Response(JSON.stringify({ title: "Unprocessable Entity", detail: "a building cannot contain a campus" }), { status: 422, headers: { "content-type": "application/json" } });
      }
      return new Response(JSON.stringify({}), { status: 200, headers: { "content-type": "application/json" } });
    });
    try {
      mount(`/web/locations/${uuidFor("lz-b1")}?tab=configure&edit=1`);
      const face = await screen.findByTestId("configure-face");
      const select = (await within(face).findByLabelText("Parent")) as HTMLSelectElement;
      fireEvent.change(select, { target: { value: uuidFor("lz-hq2") } });
      fireEvent.click(within(face).getByRole("button", { name: /save/i }));
      await waitFor(() => {
        const move = calls.find((c) => c.method === "POST" && c.url.includes(":move"));
        expect(move, "the :move custom method").toBeTruthy();
        expect(JSON.parse(move!.body).parent).toBe(uuidFor("lz-hq2"));
        // the refusal keeps edit mode and says why, in place
        expect(within(face).getByRole("alert").textContent).toContain("cannot contain");
      });
    } finally {
      stub.mockRestore();
    }
  });
});

describe("the miss face (#800)", () => {
  it("an address matching no location renders the explicit miss, not a silent fallback", async () => {
    mount("/web/locations/no-such-place");
    expect(await screen.findByText(/No location answers this address/)).toBeTruthy();
    expect(screen.queryByText("Contained systems")).toBeNull();
  });
});

// #782 on the workspace: the location-type catalog is a separate query that
// can answer after ?edit=1 opened the editor, and this select carries no
// placeholder option, so the fallback is not even empty: the browser picks the
// FIRST type, and a save in that window silently retypes the building as a
// campus. The parent picker's option pool also churns when the catalog lands
// (the allowed_parent_types filter arrives with it), so the chosen parent must
// survive that too. Delivered by hand between two assertions, never a race.
describe("a configure select takes its value when its catalog lands (#782)", () => {
  it("keeps the stored type and parent when the type catalog answers after edit opened", async () => {
    const { qc } = mount(`/web/locations/${uuidFor("lz-b1")}?tab=configure&edit=1`, true);
    const face = await screen.findByTestId("configure-face");
    const typePicker = (await within(face).findByLabelText("Location type")) as HTMLSelectElement;
    expect(typePicker.options.length).toBe(0);
    const parentPicker = () => within(face).getByLabelText("Parent") as HTMLSelectElement;
    expect(parentPicker().value).toBe(uuidFor("lz-hq"));

    qc.setQueryData([...LOCATION_TYPES_KEY], types);

    await waitFor(() => expect(typePicker.options.length).toBe(types.length));
    expect(typePicker.value).toBe("building");
    // The filter that arrived with the catalog narrowed the parent pool to
    // campuses; the stored parent is one, and stays chosen through the churn.
    await waitFor(() => expect(parentPicker().options.length).toBe(2));
    expect(parentPicker().value).toBe(uuidFor("lz-hq"));
  });
});
