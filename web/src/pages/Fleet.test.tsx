import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, within, cleanup } from "@solidjs/testing-library";
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
    // The type chip is the location type's own word.
    expect(within(hq).getByText("campus")).toBeTruthy();
    // The verdict chip renders the server's recorded word, verbatim.
    expect(within(hq).getByText("degraded")).toBeTruthy();
    // Counts: one system, two components, three levels.
    expect(within(hq).getByText(/1 system/)).toBeTruthy();
    expect(within(hq).getByText(/2 components/)).toBeTruthy();
    expect(within(hq).getByText(/3 levels/)).toBeTruthy();

    const depot = screen.getByTestId(`band-${uuidFor("fp-depot")}`);
    expect(within(depot).getByText("Service Depot")).toBeTruthy();
    expect(within(depot).getByText("healthy")).toBeTruthy();
  });

  it("clicking a band label navigates to the root location by uuid, never by name", async () => {
    mount();
    const hq = screen.getByTestId(`band-${uuidFor("fp-hq")}`);
    fireEvent.click(within(hq).getByRole("button", { name: /Headquarters/ }));
    expect(await screen.findByTestId("location-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/locations/${uuidFor("fp-hq")}`);
  });

  it("renders a dashed hole naming the empty room, inert: clicking navigates nowhere", () => {
    mount();
    const hole = screen.getByText("Boardroom B");
    fireEvent.click(hole);
    expect(window.location.pathname).toBe("/web/fleet");
    expect(screen.queryByTestId("location-page")).toBeNull();
  });

  it("renders the zoom ladder with only the Fleet chip active", () => {
    mount();
    const ladder = screen.getByTestId("zoom-ladder");
    const chips = within(ladder).getAllByRole("button");
    expect(chips).toHaveLength(4);
    expect(chips[0].textContent).toContain("Fleet");
    expect(chips[0].getAttribute("disabled")).toBeNull();
    for (const chip of chips.slice(1)) expect(chip.hasAttribute("disabled")).toBe(true);
  });

  it("shows the inspector's totals, counting a shared component once", () => {
    mount();
    const inspector = screen.getByTestId("fleet-inspector");
    expect(within(inspector).getByText(/2 systems/)).toBeTruthy();
    expect(within(inspector).getByText(/3 components/)).toBeTruthy();
    expect(within(inspector).getByText(/2 roots/)).toBeTruthy();
    // The gap footer names how many rooms hold nothing.
    expect(within(inspector).getByText(/1 location has no system/)).toBeTruthy();
  });

  it("renders the shared breadcrumb trail, one crumb at this zoom", () => {
    mount();
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getByText("Fleet")).toBeTruthy();
  });

  it("mounts and renders its chrome when the canvas has no 2d context (jsdom)", () => {
    // jsdom's getContext returns null; the page must render every word anyway.
    mount();
    expect(screen.getByText("Headquarters")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Fleet" })).toBeTruthy();
  });
});
