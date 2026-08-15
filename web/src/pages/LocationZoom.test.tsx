import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, within, cleanup, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Locations from "./Locations";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { LOCATION_TYPES_KEY, type LocationType } from "../lib/location_types";
import { LOCATIONS_KEY } from "../lib/locations";
import { SYSTEMS_KEY } from "../lib/systems";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY } from "../lib/tags";
import { uuidFor } from "../lib/testids";

// The location zoom (#635), rendered at the identity route behind ?zoom=1
// (ADR-0125): the inventory detail stays the route's default face, and the
// param renders the canvas one level down. Child bands for every direct
// child whatever its type, the placed-here band first, system cards with the
// server's own arithmetic, holes dashed and inert.

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
    // Deep under west: west's band.
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
      assigned_to: [],
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

function mount(path = `/web/locations/${uuidFor("lz-hq")}?zoom=1`) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATION_TYPES_KEY], types);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemHealthKey(uuidFor("lz-s-lobby"))], lobbyHealth);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/locations/:id" component={Locations} />
        <Route path="/fleet" component={() => <div data-testid="fleet-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("the location zoom", () => {
  it("bands every direct child whatever its type, the placed-here band first", () => {
    mount();
    const bands = screen.getAllByTestId(/^zoomband-/);
    expect(bands[0].getAttribute("data-testid")).toBe(`zoomband-${uuidFor("lz-hq")}`);
    expect(within(bands[0]).getByText("Placed here")).toBeTruthy();
    // The area-typed child bands like any other: no fixed ladder.
    const yard = screen.getByTestId(`zoomband-${uuidFor("lz-yard")}`);
    expect(within(yard).getByText("The Yard")).toBeTruthy();
    expect(within(yard).getByText("area")).toBeTruthy();
  });

  it("a system attached to the location itself appears in the placed-here band, not among the children", () => {
    mount();
    const here = screen.getByTestId(`zoomband-${uuidFor("lz-hq")}`);
    expect(within(here).getByText("Lobby AV")).toBeTruthy();
    const west = screen.getByTestId(`zoomband-${uuidFor("lz-b1")}`);
    expect(within(west).queryByText("Lobby AV")).toBeNull();
    expect(within(west).getByText("Boardroom")).toBeTruthy();
  });

  it("a system card shows the shortfall in the server's own terms, and unstaffed reads incomplete", () => {
    mount();
    const card = screen.getByTestId(`syscard-${uuidFor("lz-s-lobby")}`);
    // The card's verdict is the server's word.
    expect(within(card).getByText("incomplete")).toBeTruthy();
    // The arithmetic is the server's own figures, rendered not recomputed.
    expect(within(card).getByText(/1 of 2 satisfying/)).toBeTruthy();
    // Nothing on the card says outage: the role's impact describes failure
    // only, and nothing has failed.
    expect(within(card).queryByText("outage")).toBeNull();
  });

  it("clicking a child band drills deeper, carrying the zoom param", async () => {
    mount();
    const west = screen.getByTestId(`zoomband-${uuidFor("lz-b1")}`);
    fireEvent.click(within(west).getByRole("button", { name: /West Building/ }));
    await waitFor(() => expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-b1")}`));
    expect(window.location.search).toContain("zoom=1");
  });

  it("a systemless leaf in the subtree renders a dashed hole, inert", () => {
    mount();
    const hole = screen.getByText("The Shed");
    fireEvent.click(hole);
    expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-hq")}`);
  });

  it("names the allowed child types beneath", () => {
    mount();
    const footer = screen.getByTestId("allowed-child-types");
    // building allows campus parents; area allows anything; room does not
    // allow campus. The footer names what THIS location may contain.
    expect(within(footer).getByText("Building")).toBeTruthy();
    expect(within(footer).getByText("Area")).toBeTruthy();
    expect(within(footer).queryByText("Room")).toBeNull();
  });

  it("the breadcrumb walks the ancestor chain and the ladder lights fleet and location", () => {
    mount(`/web/locations/${uuidFor("lz-room")}?zoom=1`);
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getByText("Headquarters")).toBeTruthy();
    expect(within(trail).getByText("West Building")).toBeTruthy();
    expect(within(trail).getByText("Boardroom A")).toBeTruthy();
    const ladder = screen.getByTestId("zoom-ladder");
    const chips = within(ladder).getAllByRole("button");
    expect(chips[1].textContent).toContain("Boardroom A");
    expect(chips[1].hasAttribute("disabled")).toBe(false);
  });

  it("without the zoom param the route renders the inventory detail, untouched", () => {
    mount(`/web/locations/${uuidFor("lz-hq")}`);
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });
});
