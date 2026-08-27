import { describe, it, expect, afterEach } from "vitest";
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
import { uuidFor } from "../lib/testids";

// The location zoom (#635), the identity route's default face (ADR-0129)
// (ADR-0126): the inventory detail stays the route's default face, and the
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

function mount(path = `/web/locations/${uuidFor("lz-hq")}`) {
  localStorage.removeItem("fleet-sumopen");
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATION_TYPES_KEY], types);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
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
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/locations/:id" component={Locations} />
        <Route path="/fleet" component={() => <div data-testid="fleet-page" />} />
        <Route path="/systems/:id" component={() => <div data-testid="system-page" />} />
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
    // The band's label button carries the name and the type chip; the card
    // beneath repeats the room name on its where-line, so scope to the button.
    const label = within(yard).getAllByRole("button").find((b) => b.textContent?.includes("area"))!;
    expect(within(label).getByText("The Yard")).toBeTruthy();
    expect(within(label).getByText("area")).toBeTruthy();
  });

  it("a system attached to the location itself appears in the placed-here band, not among the children", () => {
    mount();
    const here = screen.getByTestId(`zoomband-${uuidFor("lz-hq")}`);
    expect(within(here).getByText("Lobby AV")).toBeTruthy();
    const west = screen.getByTestId(`zoomband-${uuidFor("lz-b1")}`);
    expect(within(west).queryByText("Lobby AV")).toBeNull();
    expect(within(west).getByText("Boardroom")).toBeTruthy();
  });

  it("a system card draws the slot strip and the gap line in the server's own terms; unstaffed is an empty slot, never outage", () => {
    mount();
    const card = screen.getByTestId(`syscard-${uuidFor("lz-s-lobby")}`);
    // signage wants 2, has 1 (healthy): one filled square, one empty.
    const strip = within(card).getByTestId("slot-strip");
    expect(strip.children).toHaveLength(2);
    expect(within(card).getByText("1 required slot empty")).toBeTruthy();
    // Nothing on the card says outage: the role's impact describes failure
    // only, and nothing has failed.
    expect(within(card).queryByText("outage")).toBeNull();
  });

  it("wears the same shell as the fleet zoom: the fleet-wide summary on top, and the attention badge filters this zoom's cards", () => {
    mount();
    const rail = screen.getByTestId("fleet-summary");
    expect(screen.queryByTestId("zoom-rail")).toBeNull();
    // Two systems need attention fleet-wide (Boardroom degraded, Lobby AV incomplete).
    const attention = within(rail).getByTestId("badge-attention");
    expect(within(attention).getByText("2")).toBeTruthy();
    fireEvent.click(attention);
    // The healthy Yard AV card is filtered out of this zoom; the others stay.
    expect(screen.queryByTestId(`syscard-${uuidFor("lz-s-yard")}`)).toBeNull();
    expect(screen.getByTestId(`syscard-${uuidFor("lz-s-lobby")}`)).toBeTruthy();
  });

  // #787: the location header matches the system zoom's shape.
  it("the header wears the location's verdict, the since-line, and this subtree's needs-attention count", () => {
    mount();
    const header = screen.getByTestId("location-header");
    expect(within(header).getByText("degraded")).toBeTruthy();
    expect(within(header).getByText(/since/)).toBeTruthy();
    // Fixture: signage incomplete + boardroom degraded + horn healthy = 2.
    expect(within(header).getByText(/2 need attention/)).toBeTruthy();
  });

  it("clicking the needs-attention chip applies the worst-first verdict filter to this zoom's cards", () => {
    mount();
    expect(screen.getByTestId(`syscard-${uuidFor("lz-s-yard")}`)).toBeTruthy();
    fireEvent.click(within(screen.getByTestId("location-header")).getByRole("button", { name: /need attention/ }));
    expect(screen.queryByTestId(`syscard-${uuidFor("lz-s-yard")}`)).toBeNull();
    expect(screen.getByTestId(`syscard-${uuidFor("lz-s-board")}`)).toBeTruthy();
  });

  it("clicking a child band drills deeper, at its canonical address", async () => {
    mount();
    const west = screen.getByTestId(`zoomband-${uuidFor("lz-b1")}`);
    fireEvent.click(within(west).getByRole("button", { name: /West Building/ }));
    await waitFor(() => expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-b1")}`));
    expect(window.location.search).toBe("");
  });

  it("clicking a system card walks inward to the system zoom", async () => {
    mount();
    // The whole card is the button now.
    fireEvent.click(screen.getByTestId(`syscard-${uuidFor("lz-s-lobby")}`));
    await waitFor(() => expect(window.location.pathname).toBe(`/web/systems/${uuidFor("lz-s-lobby")}`));
    expect(window.location.search).toBe("");
  });

  it("a systemless leaf in the subtree renders as an inert + System hole naming it", () => {
    mount();
    const hole = screen.getByText(/The Shed has none/);
    fireEvent.click(hole);
    expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-hq")}`);
  });

  it("names the allowed child types on the inert + Location hole", () => {
    mount();
    const hole = screen.getByTestId("allowed-child-types");
    // building allows campus parents; area allows anything; room does not
    // allow campus. The hole names what THIS location may contain.
    expect(hole.textContent).toContain("building");
    expect(hole.textContent).toContain("area");
    expect(hole.textContent).not.toContain("room");
    fireEvent.click(screen.getByTestId("add-location-hole"));
    expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-hq")}`);
  });

  it("the breadcrumb walks the ancestor chain to the parent; the page itself is the title, not a crumb", () => {
    mount(`/web/locations/${uuidFor("lz-room")}`);
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getByText("Fleet")).toBeTruthy();
    expect(within(trail).getByText("Headquarters")).toBeTruthy();
    expect(within(trail).getByText("West Building")).toBeTruthy();
    // Boardroom A is the page: in the heading, not repeated in the trail.
    expect(within(trail).queryByText("Boardroom A")).toBeNull();
    expect(screen.getByRole("heading", { name: "Boardroom A" })).toBeTruthy();
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });

  it("a name-shaped zoom link resolves to the uuid, keeping the param (#759's rule)", async () => {
    mount(`/web/locations/west`);
    await waitFor(() => expect(window.location.pathname).toBe(`/web/locations/${uuidFor("lz-b1")}`));
    expect(window.location.search).toBe("");
    expect(screen.getByTestId(`zoomband-${uuidFor("lz-room")}`)).toBeTruthy();
  });

  it("without the zoom param the route renders the inventory detail, untouched", () => {
    mount(`/web/locations/${uuidFor("lz-hq")}`);
    expect(screen.queryByTestId("zoom-ladder")).toBeNull();
  });
});

// The summary reflects the page (#795 review): this location's subtree, never
// the fleet's numbers.
describe("the scoped summary", () => {
  it("counts this subtree: hq holds all three systems here, and the mix subject stays systems", () => {
    mount();
    const rail = screen.getByTestId("fleet-summary");
    expect(within(rail).getByText("systems")).toBeTruthy();
    expect(within(rail).getAllByText("3").length).toBeGreaterThan(0);
    expect(within(rail).getByText("children")).toBeTruthy();
  });
});

// Inside a location, the density toggle lists the subtree as rows (#798): one
// row per system, verdict first, click opens the system full screen (the
// altitude rule). The view is a URL fact, so ?view=list deep-links.
describe("the location list density", () => {
  it("?view=list renders the subtree as rows and a row opens its system", async () => {
    mount(`/web/locations/${uuidFor("lz-hq")}?view=list`);
    const rows = await screen.findByTestId("fleet-rows");
    expect(within(rows).getByText("Lobby AV")).toBeTruthy();
    expect(within(rows).getByText("Boardroom")).toBeTruthy();
    expect(within(rows).getByText("Yard AV")).toBeTruthy();
    fireEvent.click(within(rows).getByRole("button", { name: /Boardroom/ }));
    expect(await screen.findByTestId("system-page")).toBeTruthy();
    expect(window.location.pathname).toBe(`/web/systems/${uuidFor("lz-s-board")}`);
  });

  it("offers the toggle on the canvas face and points it at the list", async () => {
    mount();
    const toggle = screen.getByTestId("view-toggle");
    fireEvent.click(within(toggle).getByRole("button", { name: /list/i }));
    await waitFor(() => expect(window.location.search).toContain("view=list"));
  });
});
