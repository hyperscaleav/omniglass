import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useSearchParams } from "@solidjs/router";
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
        <Route path="/systems/create" component={() => <div data-testid="system-create">{underParam()}</div>} />
        <Route path="/locations/create" component={() => <div data-testid="location-create">{underParam()}</div>} />
      </Router>
    </QueryClientProvider>
  ));
}

// The create stubs report where the form was told to land, read through the
// router rather than window.location, which jsdom does not keep in step.
function underParam() {
  const [params] = useSearchParams();
  const v = params.under;
  return Array.isArray(v) ? (v[0] ?? "") : (v ?? "");
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

  it("lands a name-shaped address too, which the operator guide links by name", async () => {
    mount("/web/explore?node=west");
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
    fireEvent.change(screen.getByLabelText("Labels"), { target: { value: "off" } });
    expect(screen.getByTestId("explore-status").textContent).toContain("labels off (forced)");
  });

  it("counts the rooms in front of the operator, not the fleet's total", async () => {
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
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "bands" } });
    const after = screen.getByTestId(`explore-section-${uuidFor("hq")}`).querySelectorAll("[data-dot]").length;
    expect(after).toBe(before);
  });

  it("remembers the renderer per browser but never the drilled node", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "bands" } });
    cleanup();
    mount("/web/explore", owner, undefined, true);
    expect((await screen.findByLabelText("View")) as HTMLSelectElement).toHaveProperty("value", "bands");
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

describe("presets", () => {
  it("ships a set named after jobs, and applying one moves the controls", async () => {
    mount();
    const bar = await screen.findByTestId("explore-presets");
    expect(within(bar).getByRole("button", { name: "Fleet overview" })).toBeTruthy();
    fireEvent.click(within(bar).getByRole("button", { name: "Standards audit" }));
    expect(await screen.findByTestId("explore-matrix")).toBeTruthy();
    expect((screen.getByLabelText("View") as HTMLSelectElement).value).toBe("matrix");
  });

  it("lights the chip that matches the controls, and only that one", async () => {
    mount();
    const bar = await screen.findByTestId("explore-presets");
    expect(within(bar).getByRole("button", { name: "Fleet overview" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "mosaic" } });
    expect(within(bar).getByRole("button", { name: "Shape of the fleet" }).getAttribute("aria-pressed")).toBe("true");
    expect(within(bar).getByRole("button", { name: "Fleet overview" }).getAttribute("aria-pressed")).toBe("false");
  });

  it("carries the filter as well as the drawing, since triage is a way of looking", async () => {
    mount();
    await screen.findByTestId("explore-presets");
    fireEvent.click(screen.getByRole("button", { name: "Morning triage" }));
    await waitFor(() =>
      expect((screen.getByLabelText("Only what needs attention") as HTMLInputElement).checked).toBe(true),
    );
    expect(screen.queryByTestId(`explore-section-${uuidFor("depot")}`)).toBeNull();
  });

  it("saves the current view under a name, then forgets it", async () => {
    mount();
    await screen.findByTestId("explore-presets");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "bands" } });
    fireEvent.click(screen.getByRole("button", { name: "Save this view" }));
    const box = await screen.findByLabelText("Name this view");
    fireEvent.input(box, { target: { value: "My sweep" } });
    fireEvent.keyDown(box, { key: "Enter" });

    const bar = await screen.findByTestId("explore-presets");
    expect(within(bar).getByRole("button", { name: "My sweep" })).toBeTruthy();

    fireEvent.click(within(bar).getByRole("button", { name: "Forget My sweep" }));
    expect(within(screen.getByTestId("explore-presets")).queryByRole("button", { name: "My sweep" })).toBeNull();
  });

  it("brings a saved view back in the next session", async () => {
    mount();
    await screen.findByTestId("explore-presets");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "matrix" } });
    fireEvent.click(screen.getByRole("button", { name: "Save this view" }));
    const box = await screen.findByLabelText("Name this view");
    fireEvent.input(box, { target: { value: "Audit" } });
    fireEvent.keyDown(box, { key: "Enter" });
    cleanup();

    mount("/web/explore", owner, undefined, true);
    const bar = await screen.findByTestId("explore-presets");
    fireEvent.click(within(bar).getByRole("button", { name: "Audit" }));
    expect(await screen.findByTestId("explore-matrix")).toBeTruthy();
  });

  it("falls back to the fleet when a saved view names a node that is gone", async () => {
    localStorage.setItem(
      "explore-presets",
      JSON.stringify([{ name: "Stale", why: "", state: { renderer: "cards", node: uuidFor("vanished") } }]),
    );
    mount("/web/explore", owner, undefined, true);
    const bar = await screen.findByTestId("explore-presets");
    fireEvent.click(within(bar).getByRole("button", { name: "Stale" }));
    // Landing on a blank page would be worse than landing somewhere real.
    expect(await screen.findByRole("button", { name: "Open West Building" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "All locations" })).toBeDisabled();
  });
});

describe("the mosaic and matrix renderers", () => {
  it("draws the mosaic over the same sections, one frame per root", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "mosaic" } });
    const mosaic = await screen.findByTestId("explore-mosaic");
    // Every cut node is a tile, and a tile carries its counts for a reader.
    expect(mosaic.querySelectorAll("[data-tile]").length).toBeGreaterThan(0);
    expect(within(mosaic).getByLabelText(/West Building, \d+ systems/)).toBeTruthy();
  });

  it("drills from a mosaic tile, the same as from a card", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "mosaic" } });
    const mosaic = await screen.findByTestId("explore-mosaic");
    fireEvent.click(within(mosaic).getByLabelText(/West Building, /));
    expect(await screen.findByRole("button", { name: "Open Media Lab" })).toBeTruthy();
  });

  it("pivots place against standard, rows following each root's own cut", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "matrix" } });
    const matrix = await screen.findByTestId("explore-matrix");
    expect(within(matrix).getByRole("columnheader", { name: "Place" })).toBeTruthy();
    // hq has two buildings so it cuts there; depot has a single room, so it is
    // its own row. Two roots, two different cuts, in one table.
    expect(within(matrix).getByRole("button", { name: "Open West Building" })).toBeTruthy();
    expect(within(matrix).getByRole("button", { name: "Open Service Depot" })).toBeTruthy();
    expect(within(matrix).queryByRole("button", { name: "Open Bay 1" })).toBeNull();
  });

  it("switches renderer without changing which systems are in scope", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "mosaic" } });
    expect(await screen.findByTestId("explore-mosaic")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("View"), { target: { value: "cards" } });
    expect(await screen.findByRole("button", { name: "Open West Building" })).toBeTruthy();
    expect(screen.queryByTestId("explore-mosaic")).toBeNull();
  });
});

describe("create where you stand", () => {
  it("offers both creates on a drilled node, placed at that node", async () => {
    mount(`/web/explore?node=${uuidFor("west")}`);
    const head = await screen.findByTestId("explore-section-head");
    expect(within(head).getByRole("button", { name: "+ Location here" })).toBeTruthy();
    fireEvent.click(within(head).getByRole("button", { name: "+ System here" }));
    const created = await screen.findByTestId("system-create");
    expect(created.textContent).toBe(uuidFor("west"));
  });

  it("offers neither at the fleet level, where there is nowhere to stand", async () => {
    mount();
    await screen.findByTestId("explore-controls");
    expect(screen.queryByRole("button", { name: "+ System here" })).toBeNull();
  });

  it("hides a create from a caller without the verb", async () => {
    mount(`/web/explore?node=${uuidFor("west")}`, partial);
    const head = await screen.findByTestId("explore-section-head");
    expect(within(head).queryByRole("button", { name: "+ Location here" })).toBeNull();
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
