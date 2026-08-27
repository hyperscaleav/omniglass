import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Fleet from "./Fleet";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The fleet zoom's chrome, all of it DOM (#633): the band label column, the
// verdict chip, the type chip, the counts, the dashed holes, the zoom ladder.
// Six of the eight acceptance criteria name text or a click target, and every
// one of them is provable here in jsdom because none of it is painted on the
// canvas. What the canvas paints is proven at the unit tier (paintGroups) and
// by Playwright; jsdom has no 2d context and this suite never pretends it
// does.

const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

const loc = (handle: string, name: string, label: string, type: string, parent: string, verdict: string) => ({
  id: uuidFor(handle),
  name,
  label,
  location_type: type,
  location_type_id: uuidFor(`ft-${type}`),
  parent: parent ? uuidFor(parent) : "",
  verdict,
});

// Two roots of different shapes; hq's own recorded verdict (degraded)
// deliberately disagrees with nothing here, but the fixture's boardroom
// system is degraded so both sources say degraded; the recorded-verdict
// distinction is pinned at the unit tier where the two can be made to
// disagree cleanly.
const view: FleetView = {
  locations: [
    loc("fp-hq", "hq", "Headquarters", "campus", "", "degraded"),
    loc("fp-b1", "west", "West Building", "building", "fp-hq", "degraded"),
    loc("fp-room", "boardroom-a", "Boardroom A", "room", "fp-b1", "degraded"),
    // A leaf with no system: the dashed hole.
    loc("fp-empty", "boardroom-b", "Boardroom B", "room", "fp-b1", "healthy"),
    loc("fp-depot", "depot", "Service Depot", "building", "", "healthy"),
    loc("fp-bay", "bay-1", "", "room", "fp-depot", "healthy"),
  ],
  systems: [
    {
      id: uuidFor("fp-s-board"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("fp-room"),
      verdict: "degraded",
      dots: [
        { component: uuidFor("fp-c-bar"), name: "videobar-1", verdict: "degraded", primary: true, shared: false },
        { component: uuidFor("fp-c-panel"), name: "display-1", verdict: "healthy", primary: true, shared: false },
      ],
    },
    {
      id: uuidFor("fp-s-bay"),
      name: "bay-av",
      label: "Bay AV",
      location: uuidFor("fp-bay"),
      verdict: "healthy",
      dots: [{ component: uuidFor("fp-c-baybar"), name: "videobar-1", verdict: "healthy", primary: true, shared: false }],
    },
  ],
} as unknown as FleetView;

function mount(data: FleetView = view) {
  // The summary rail persists its open state per page; start each mount collapsed.
  localStorage.removeItem("fleet-sumopen");
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], data);
  qc.setQueryData([...ME_KEY], me);
  window.history.pushState({}, "", "/web/fleet");
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/fleet" component={Fleet} />
        <Route path="/locations/:id" component={() => <div data-testid="location-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("the fleet zoom's bands", () => {
  it("renders one band per root location with its verdict, counts, and type chip", () => {
    mount();
    const hq = screen.getByTestId(`band-${uuidFor("fp-hq")}`);
    expect(within(hq).getByText("Headquarters")).toBeTruthy();
    expect(within(hq).getByText("campus")).toBeTruthy();
    expect(within(hq).getByText("degraded")).toBeTruthy();
    expect(within(hq).getByText(/1 system/)).toBeTruthy();
    expect(within(hq).getByText(/2 components/)).toBeTruthy();
    // Depth is a tile on the summary rail, not a band subtitle: the band line
    // stays one line wide so the label column never wraps.
    expect(within(hq).queryByText(/levels/)).toBeNull();
    const depot = screen.getByTestId(`band-${uuidFor("fp-depot")}`);
    expect(within(depot).getByText("Service Depot")).toBeTruthy();
    expect(within(depot).getByText("healthy")).toBeTruthy();
  });

  it("clicking a band label navigates to the root location by uuid, the default face (ADR-0129)", async () => {
    mount();
    const hq = screen.getByTestId(`band-${uuidFor("fp-hq")}`);
    fireEvent.click(within(hq).getByRole("button", { name: /Headquarters/ }));
    expect(await screen.findByTestId("location-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/locations/${uuidFor("fp-hq")}`);
    expect(window.location.search).toBe("");
  });

  it("renders a dashed hole naming the empty room, inert: clicking navigates nowhere", () => {
    mount();
    const hole = screen.getByText("Boardroom B");
    fireEvent.click(hole);
    expect(window.location.pathname).toBe("/web/fleet");
    expect(screen.queryByTestId("location-page")).toBeNull();
  });


  it("shows the summary rail over systems, the Locations page's shape: mix badge, attention, gaps, components, roots", () => {
    mount();
    const rail = screen.getByTestId("fleet-summary");
    expect(within(rail).getByText("systems")).toBeTruthy();
    const attention = within(rail).getByTestId("badge-attention");
    // One degraded system needs attention.
    expect(within(attention).getByText("1")).toBeTruthy();
    expect(within(rail).getByText(/gap$/)).toBeTruthy();
    expect(within(rail).getByText("components")).toBeTruthy();
    expect(within(rail).getByText("roots")).toBeTruthy();
    // No right rail: the summary is where its facts went.
    expect(screen.queryByTestId("zoom-rail")).toBeNull();
    // Expanding shows the verdict donut with its legend and the count tiles.
    fireEvent.click(within(rail).getByRole("button", { name: /expand summary/i }));
    expect(within(rail).getByText("Summary")).toBeTruthy();
    expect(within(rail).getByText(/locations? with no system/)).toBeTruthy();
    expect(within(rail).getByText(/levels deep/)).toBeTruthy();
  });

  it("the attention badge filters the canvas to what needs it, and again clears it", () => {
    mount();
    fireEvent.click(screen.getByTestId("badge-attention"));
    // The depot's only system is healthy: filtered out, its band stays.
    const depot = screen.getByTestId(`band-${uuidFor("fp-depot")}`);
    expect(within(depot).getByRole("img").getAttribute("aria-label")).toContain("Service Depot");
    fireEvent.click(screen.getByTestId("badge-attention"));
    expect(screen.getByTestId(`band-${uuidFor("fp-depot")}`)).toBeTruthy();
  });

  it("renders the inert add-location hole", () => {
    mount();
    fireEvent.click(screen.getByTestId("add-root-hole"));
    expect(window.location.pathname).toBe("/web/fleet");
  });

  it("draws a root that holds only holes: a site nobody commissioned is a band of gaps, not invisible", () => {
    const withEmptyRoot = {
      ...view,
      locations: [
        ...view.locations!,
        loc("fp-new", "north-annex", "North Annex", "building", "", "healthy"),
        loc("fp-new-r1", "room-1", "Room 1", "room", "fp-new", "healthy"),
      ],
    } as unknown as FleetView;
    mount(withEmptyRoot);
    const band = screen.getByTestId(`band-${uuidFor("fp-new")}`);
    expect(within(band).getByText("North Annex")).toBeTruthy();
    expect(within(band).getByText(/0 systems/)).toBeTruthy();
    expect(within(band).getByText("Room 1")).toBeTruthy();
  });

  it("is the root of the walk: a title and no breadcrumb, no ladder", () => {
    mount();
    expect(screen.getByRole("heading", { name: "Fleet" })).toBeTruthy();
    expect(screen.queryByTestId("breadcrumb")).toBeNull();
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });

  it("mounts and renders its chrome when the canvas has no 2d context (jsdom)", () => {
    mount();
    expect(within(screen.getByTestId(`band-${uuidFor("fp-hq")}`)).getByText("Headquarters")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Fleet" })).toBeTruthy();
  });
});

// The density toggle (#798, ADR-0129's "tables survive as a list-density
// toggle"): the fleet is one door with two densities. The canvas is the
// default; ?view=list swaps the body for the classic index faces under kind
// tabs (Locations, Systems, Components), the old index pages re-homed. The
// view is a URL fact like ?tab= (the #763 deep-link rule).
describe("the fleet density toggle", () => {
  it("defaults to the canvas and offers the list toggle", async () => {
    mount();
    expect(screen.getByTestId(`band-${uuidFor("fp-hq")}`)).toBeTruthy();
    const toggle = screen.getByTestId("view-toggle");
    fireEvent.click(within(toggle).getByRole("button", { name: /list/i }));
    await waitFor(() => expect(window.location.search).toContain("view=list"));
  });

  it("?view=list renders the kind tabs and mounts the locations face first, canvas gone", async () => {
    localStorage.removeItem("fleet-sumopen");
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...FLEET_VIEW_KEY], view);
    qc.setQueryData([...ME_KEY], me);
    window.history.pushState({}, "", "/web/fleet?view=list");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router base="/web">
          <Route path="/fleet" component={Fleet} />
        </Router>
      </QueryClientProvider>
    ));
    const rail = screen.getByTestId("tab-rail");
    expect(within(rail).getByRole("tab", { name: "Locations" }).getAttribute("aria-selected")).toBe("true");
    expect(within(rail).getByRole("tab", { name: "Systems" })).toBeTruthy();
    expect(within(rail).getByRole("tab", { name: "Components" })).toBeTruthy();
    expect(screen.getByTestId("fleet-list-face")).toBeTruthy();
    expect(screen.queryByTestId(`band-${uuidFor("fp-hq")}`)).toBeNull();
    // The toggle points back: leaving the list clears view and kind together.
    const toggle = screen.getByTestId("view-toggle");
    fireEvent.click(within(toggle).getByRole("button", { name: /canvas/i }));
    await waitFor(() => expect(window.location.search).not.toContain("view=list"));
  });

  it("?kind=systems selects the systems tab and its face", () => {
    localStorage.removeItem("fleet-sumopen");
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...FLEET_VIEW_KEY], view);
    qc.setQueryData([...ME_KEY], me);
    window.history.pushState({}, "", "/web/fleet?view=list&kind=systems");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router base="/web">
          <Route path="/fleet" component={Fleet} />
        </Router>
      </QueryClientProvider>
    ));
    const rail = screen.getByTestId("tab-rail");
    expect(within(rail).getByRole("tab", { name: "Systems" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByTestId("fleet-list-face")).toBeTruthy();
  });
});
