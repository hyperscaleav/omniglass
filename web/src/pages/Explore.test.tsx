import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Explore from "./Explore";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { SYSTEMS_KEY } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { SYSTEM_TYPES_KEY } from "../lib/system_types";
import { TAGS_KEY } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Explore page (#839): one card per CUT NODE, where the cut is chosen per
// root from the types that root contains, so a campus of buildings and a
// two-level annex sit side by side with each card naming its own type. Labels
// and room boxes are budgets rather than preferences. The fixture is
// deliberately uneven: hq cuts at building, depot has no container level and is
// its own card, and one system hangs above the cut on the campus itself.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const partial: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["location:read", "system:read", "tag:read"], grants: [] };

const loc = (handle: string, name: string, label: string, type: string, parent: string, verdict: string | null) => ({
  id: uuidFor(handle), name, label, location_type: type, location_type_id: uuidFor(`t-${type}`), parent: parent ? uuidFor(parent) : "", verdict,
});
const sys = (handle: string, name: string, label: string, location: string, verdict: string) => ({
  id: uuidFor(handle), name, label, location: uuidFor(location), verdict, dots: [],
});
const view: FleetView = {
  locations: [
    loc("hq", "hq", "Headquarters", "campus", "", "degraded"),
    loc("west", "west", "West Building", "building", "hq", "degraded"),
    loc("l2", "level-2", "Level 2", "floor", "west", "healthy"),
    loc("huddle-room", "huddle-room", "Huddle Room", "room", "l2", "healthy"),
    loc("media-lab", "media-lab", "Media Lab", "room", "west", "degraded"),
    loc("storage", "storage", "Storage", "room", "west", null),
    loc("east", "east", "East Building", "building", "hq", "healthy"),
    loc("l1", "level-1", "Level 1", "floor", "east", "healthy"),
    loc("lab", "lab", "Lab", "room", "l1", "healthy"),
    loc("depot", "depot", "Service Depot", "building", "", "healthy"),
    loc("bay-1", "bay-1", "Bay 1", "room", "depot", "healthy"),
  ],
  systems: [
    sys("s-huddle", "huddle", "Huddle", "huddle-room", "healthy"),
    sys("s-class", "classroom", "Classroom", "media-lab", "degraded"),
    sys("s-class2", "classroom-2", "Classroom 2", "media-lab", "healthy"),
    sys("s-lab", "lab-av", "Lab AV", "lab", "healthy"),
    sys("s-bay", "bay-av", "Bay AV", "bay-1", "healthy"),
    // attached to the campus itself: it belongs to no building and no room
    sys("s-paging", "paging", "Campus Paging", "hq", "healthy"),
    // readable, but its location is not in this payload
    { id: uuidFor("s-hidden"), name: "hidden", label: "Hidden AV", location: uuidFor("elsewhere"), verdict: "healthy", dots: [] },
  ],
} as unknown as FleetView;

function mount(path = "/web/explore", me: Me = owner, storedFace?: "table", keepPrefs = false) {
  localStorage.removeItem("explore-face");
  if (!keepPrefs) localStorage.removeItem("explore-prefs");
  if (storedFace) localStorage.setItem("explore-face", storedFace);
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], view.systems!.map((s) => ({ id: s.id, name: s.name, label: s.label, location_id: s.location, standard: "", actions: ["update", "rename"] })));
  qc.setQueryData([...LOCATIONS_KEY], view.locations!.map((l) => ({ id: l.id, name: l.name, label: l.label, location_type: l.location_type, parent_id: l.parent || null, actions: ["update", "move", "rename"] })));
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], []);
  qc.setQueryData([...SYSTEM_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  for (const s of view.systems!) qc.setQueryData(["system-properties", s.id], []);
  for (const l of view.locations!) qc.setQueryData(["location-properties", l.id], []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/explore" component={Explore} />
        <Route path="/systems/:id" component={() => <div data-testid="system-page" />} />
        <Route path="/locations/:id" component={() => <div data-testid="location-page" />} />
        <Route path="/systems/create" component={() => <div data-testid="system-create">{window.location.search}</div>} />
        <Route path="/locations/create" component={() => <div data-testid="location-create">{window.location.search}</div>} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

const section = (id: string) => screen.getByTestId(`explore-section-${uuidFor(id)}`);

describe("the cut decides the cards", () => {
  it("cards a campus at its buildings, each card naming its own type", async () => {
    mount();
    const hq = await screen.findByTestId(`explore-section-${uuidFor("hq")}`);
    expect(within(hq).getByRole("button", { name: "Open West Building" })).toBeTruthy();
    expect(within(hq).getAllByText(/building ·/).length).toBe(2);
  });

  it("makes a root with no container level its own single card", async () => {
    mount();
    await screen.findByTestId(`explore-section-${uuidFor("hq")}`);
    const depot = section("depot");
    expect(within(depot).getByRole("button", { name: "Open Service Depot" })).toBeTruthy();
  });

  it("puts a system attached above the cut in its own strip, not in a card", async () => {
    mount();
    await screen.findByTestId(`explore-section-${uuidFor("hq")}`);
    const strip = within(section("hq")).getByTestId("explore-above-cut");
    expect(strip.textContent).toContain("1 system attached above this level");
    expect(within(strip).getByLabelText(/Campus Paging/)).toBeTruthy();
  });

  it("surfaces a readable system whose location the caller cannot read", async () => {
    mount();
    const unplaced = await screen.findByTestId("explore-unplaced");
    expect(within(unplaced).getByLabelText(/Hidden AV/)).toBeTruthy();
  });
});

describe("the drill", () => {
  it("opens a card into its children and back out again", async () => {
    mount();
    const hq = await screen.findByTestId(`explore-section-${uuidFor("hq")}`);
    fireEvent.click(within(hq).getByRole("button", { name: "Open West Building" }));
    expect(await screen.findByRole("button", { name: "Open Level 2" })).toBeTruthy();
    expect(await screen.findByRole("button", { name: "Open Media Lab" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "All locations" }));
    expect(await screen.findByRole("button", { name: "Open West Building" })).toBeTruthy();
  });

  it("carries the drilled node in the URL so a link lands on it", async () => {
    mount(`/web/explore?node=${uuidFor("west")}`);
    expect(await screen.findByRole("button", { name: "Open Media Lab" })).toBeTruthy();
  });

  it("opens a system when its dot is clicked", async () => {
    mount();
    const hq = await screen.findByTestId(`explore-section-${uuidFor("hq")}`);
    fireEvent.click(within(hq).getByLabelText(/Campus Paging/));
    expect(await screen.findByTestId("system-page")).toBeTruthy();
  });
});

describe("the label budget", () => {
  it("affords labels at this size, and says so in the status line", async () => {
    mount();
    const status = await screen.findByTestId("explore-status");
    expect(status.textContent).toContain("rooms in view");
    expect(status.textContent).toContain("labels on (auto)");
  });

  it("lets the operator force them off, which the status line then reports", async () => {
    mount();
    await screen.findByTestId("explore-status");
    fireEvent.click(within(screen.getByTestId("explore-controls")).getByRole("button", { name: "off" }));
    expect(screen.getByTestId("explore-status").textContent).toContain("labels off (forced)");
  });

  it("counts the rooms in front of the operator, not the estate's total", async () => {
    mount(`/web/explore?node=${uuidFor("west")}`);
    const status = await screen.findByTestId("explore-status");
    // west holds Level 2 (with a room), Media Lab and Storage: three leaves
    expect(status.textContent).toContain("3 rooms in view");
  });
});

describe("the controls change how it is drawn, never what is in it", () => {
  it("switches renderer without changing which systems are shown", async () => {
    mount();
    const before = (await screen.findByTestId(`explore-section-${uuidFor("hq")}`)).querySelectorAll("[data-dot]").length;
    fireEvent.click(within(screen.getByTestId("explore-controls")).getByRole("button", { name: "Bands" }));
    const after = screen.getByTestId(`explore-section-${uuidFor("hq")}`).querySelectorAll("[data-dot]").length;
    expect(after).toBe(before);
  });

  it("remembers the renderer per browser but never the drilled node", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.click(screen.getByRole("button", { name: "Bands" }));
    cleanup();
    mount("/web/explore", owner, undefined, true);
    expect((await screen.findByRole("button", { name: "Bands" })).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "All locations" })).toBeDisabled();
  });

  it("filters to what needs attention, and drops a section with nothing left", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.click(screen.getByLabelText("Only what needs attention"));
    expect(await screen.findByTestId(`explore-section-${uuidFor("hq")}`)).toBeTruthy();
    expect(screen.queryByTestId(`explore-section-${uuidFor("depot")}`)).toBeNull();
  });

  it("carries the filter in the URL so a link reproduces it", async () => {
    mount("/web/explore?attention=1");
    await screen.findByTestId("explore-controls");
    expect((screen.getByLabelText("Only what needs attention") as HTMLInputElement).checked).toBe(true);
  });
});

describe("the faces", () => {
  it("wears today's list face behind ?face=table, and returns to the fleet", async () => {
    mount("/web/explore?face=table");
    expect(await screen.findByRole("tab", { name: "Locations" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "fleet" }));
    expect(await screen.findByTestId("explore-controls")).toBeTruthy();
  });

  it("never lets a stored table face override a ?node= address", async () => {
    mount(`/web/explore?node=${uuidFor("west")}`, owner, "table");
    expect(await screen.findByRole("button", { name: "Open Media Lab" })).toBeTruthy();
  });

  it("still applies a stored table face to a bare /explore", async () => {
    mount("/web/explore", owner, "table");
    expect(await screen.findByRole("tab", { name: "Locations" })).toBeTruthy();
  });

  it("offers a kind tab only to a caller who may read that kind", async () => {
    mount("/web/explore?face=table", partial, "table");
    expect(await screen.findByRole("tab", { name: "Locations" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Systems" })).toBeTruthy();
    expect(screen.queryByRole("tab", { name: "Components" })).toBeNull();
  });
});
