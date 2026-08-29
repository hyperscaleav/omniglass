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

// The Explore page (#826 slice 2): a depth-agnostic Miller-column drill down
// the location tree to a system, with the glance as its rightmost column.
// The tree's shape is the fleet view's parent pointers, nothing assumed.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["location:read", "system:read", "component:read", "tag:read"], grants: [] };

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
    loc("depot", "depot", "Service Depot", "building", "", "healthy"),
    loc("bay-1", "bay-1", "Bay 1", "room", "depot", "healthy"),
  ],
  systems: [
    sys("s-huddle", "huddle", "Huddle", "huddle-room", "healthy"),
    sys("s-class", "classroom", "Classroom", "media-lab", "degraded"),
    sys("s-class2", "classroom-2", "Classroom 2", "media-lab", "healthy"),
    sys("s-bay", "bay-av", "Bay AV", "bay-1", "healthy"),
  ],
} as unknown as FleetView;

function mount(path = "/web/explore", me: Me = owner, storedFace?: "table") {
  localStorage.removeItem("explore-face");
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

const column = (header: string) => screen.getByTestId(`explore-column-${header}`);

describe("the columns", () => {
  it("opens on the parentless locations, whatever their types, under a Locations header", async () => {
    mount();
    const roots = await screen.findByTestId("explore-column-Locations");
    expect(within(roots).getByText("Headquarters")).toBeTruthy();
    expect(within(roots).getByText("Service Depot")).toBeTruthy();
    expect(within(roots).getByText("building")).toBeTruthy();
    expect(screen.queryByText("West Building")).toBeNull();
  });

  it("drills one column per click, each headed by the node whose children it lists", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Headquarters/ }));
    fireEvent.click(within(column("Headquarters")).getByRole("button", { name: /West Building/ }));
    const west = column("West Building");
    expect(within(west).getByText("Level 2")).toBeTruthy();
    expect(within(west).getByText("Media Lab")).toBeTruthy();
    expect(within(west).getByText("Storage")).toBeTruthy();
    expect(within(west).getByText("empty")).toBeTruthy();
  });

  it("collapses a lone system into its location's row, and selecting it opens the system glance", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Headquarters/ }));
    fireEvent.click(within(column("Headquarters")).getByRole("button", { name: /West Building/ }));
    fireEvent.click(within(column("West Building")).getByRole("button", { name: /Level 2/ }));
    const l2 = column("Level 2");
    const row = within(l2).getByRole("button", { name: /Huddle/ });
    expect(within(row).getByText(/in Huddle Room/)).toBeTruthy();
    fireEvent.click(row);
    const glance = await screen.findByTestId("explore-glance");
    expect(within(glance).getByTestId("glance-title").textContent).toBe("Huddle");
    expect(within(glance).getByText("Headquarters / West Building / Level 2 / Huddle Room")).toBeTruthy();
    expect(within(glance).getByRole("button", { name: /open workspace/i })).toBeTruthy();
    expect(await within(glance).findByTestId("entity-form")).toBeTruthy();
    expect(window.location.search).toContain(`node=${uuidFor("s-huddle")}`);
  });

  it("pins the glance beside the column strip, outside the strip's own scroller", async () => {
    // Four columns deep, the strip overflows and scrolls; a glance rendered
    // inside the scroller rode out of view with it (the first capture of the
    // fleet shot clipped the path and the roles). The glance is a sibling.
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Headquarters/ }));
    fireEvent.click(within(column("Headquarters")).getByRole("button", { name: /West Building/ }));
    fireEvent.click(within(column("West Building")).getByRole("button", { name: /Level 2/ }));
    fireEvent.click(within(column("Level 2")).getByRole("button", { name: /Huddle/ }));
    const glance = await screen.findByTestId("explore-glance");
    const strip = screen.getByTestId("explore-columns");
    expect(strip.contains(column("Level 2"))).toBe(true);
    expect(strip.contains(glance)).toBe(false);
    expect(glance.parentElement).toBe(strip.parentElement);
  });

  it("keeps a room with two systems as a node whose column lists both", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Headquarters/ }));
    fireEvent.click(within(column("Headquarters")).getByRole("button", { name: /West Building/ }));
    fireEvent.click(within(column("West Building")).getByRole("button", { name: /Media Lab/ }));
    const lab = column("Media Lab");
    expect(within(lab).getByRole("button", { name: /Classroom 2/ })).toBeTruthy();
    expect(within(lab).getAllByRole("button").length).toBe(2);
  });
});

describe("the location glance", () => {
  it("selecting a location shows its own glance with the roll-up, Open location, and the two creates under it", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Headquarters/ }));
    fireEvent.click(within(column("Headquarters")).getByRole("button", { name: /West Building/ }));
    const glance = await screen.findByTestId("explore-glance");
    expect(within(glance).getByTestId("glance-title").textContent).toBe("West Building");
    expect(within(glance).getByText(/3 systems/)).toBeTruthy();
    expect(within(glance).getByRole("button", { name: /open location/i })).toBeTruthy();
    fireEvent.click(within(glance).getByRole("button", { name: /system here/i }));
    await screen.findByTestId("system-create");
    expect(window.location.pathname).toBe("/web/systems/create");
    expect(window.location.search).toContain(`under=${uuidFor("west")}`);
  });

  it("hides the creates and Edit from a caller without the verbs", async () => {
    mount("/web/explore", viewer);
    fireEvent.click(await screen.findByRole("button", { name: /Headquarters/ }));
    const glance = await screen.findByTestId("explore-glance");
    expect(within(glance).queryByRole("button", { name: /system here/i })).toBeNull();
    expect(within(glance).queryByRole("button", { name: /location here/i })).toBeNull();
    expect(within(glance).queryByRole("button", { name: /^edit$/i })).toBeNull();
    expect(within(glance).getByRole("button", { name: /open location/i })).toBeTruthy();
  });
});

describe("search and addresses", () => {
  it("typing replaces the columns with hits carrying their paths, systems first; choosing one rebuilds the columns", async () => {
    mount();
    await screen.findByTestId("explore-column-Locations");
    fireEvent.input(screen.getByRole("searchbox"), { target: { value: "class" } });
    const hits = await screen.findByTestId("explore-hits");
    const rows = within(hits).getAllByRole("button");
    expect(rows[0].textContent).toContain("Classroom");
    expect(rows[0].textContent).toContain("Headquarters / West Building / Media Lab");
    expect(screen.queryByTestId("explore-column-Locations")).toBeNull();
    fireEvent.click(rows[0]);
    expect(await screen.findByTestId("explore-column-Media Lab")).toBeTruthy();
    expect(within(await screen.findByTestId("explore-glance")).getByTestId("glance-title").textContent).toBe("Classroom");
  });

  it("lands a ?node= deep link on the node with its columns rebuilt", async () => {
    mount(`/web/explore?node=${uuidFor("s-bay")}`);
    expect(await screen.findByTestId("explore-column-Service Depot")).toBeTruthy();
    const glance = await screen.findByTestId("explore-glance");
    expect(within(glance).getByTestId("glance-title").textContent).toBe("Bay AV");
  });

  it("?face=table wears today's list face behind the toggle, and the toggle returns to the tree", async () => {
    mount("/web/explore?face=table");
    expect(await screen.findByTestId("fleet-list-face")).toBeTruthy();
    expect(screen.queryByTestId("explore-column-Locations")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^tree$/i }));
    expect(await screen.findByTestId("explore-column-Locations")).toBeTruthy();
    expect(window.location.search).not.toContain("face=table");
  });
});

describe("the face memory and the address", () => {
  it("a stored table face never overrides a ?node= deep link: the address wins", async () => {
    mount(`/web/explore?node=${uuidFor("s-bay")}`, owner, "table");
    expect(await screen.findByTestId("explore-column-Service Depot")).toBeTruthy();
    expect(screen.queryByTestId("fleet-list-face")).toBeNull();
    expect(window.location.search).not.toContain("face=table");
  });

  it("a stored table face still applies to a bare /explore", async () => {
    mount("/web/explore", owner, "table");
    expect(await screen.findByTestId("fleet-list-face")).toBeTruthy();
  });
});
