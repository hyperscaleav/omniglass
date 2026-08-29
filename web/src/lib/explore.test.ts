import { describe, expect, it } from "vitest";
import { columnsFor, pathLabel, rowsFor, searchTree, subtreeVerdict } from "./explore";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The explorer's pure core (#826 slice 2): a column is nothing but "this
// node's children" at any depth, locations of any type first, systems after;
// a location with one system and no sub-locations collapses into that
// system's row; a location's verdict rolls up from every system under it;
// search matches by name or path fragment, systems first.

const loc = (handle: string, name: string, label: string, type: string, parent: string, verdict: string | null) => ({
  id: uuidFor(handle), name, label, location_type: type, location_type_id: uuidFor(`t-${type}`), parent: parent ? uuidFor(parent) : "", verdict,
});
const sys = (handle: string, name: string, label: string, location: string, verdict: string) => ({
  id: uuidFor(handle), name, label, location: uuidFor(location), verdict, dots: [],
});

// Deliberately uneven: a room straight under a building, a wing between
// building and floor, a building as a root beside campuses.
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

describe("rowsFor", () => {
  it("lists the parentless locations, of any type, as the first column", () => {
    const rows = rowsFor(view, null);
    expect(rows.map((r) => r.label)).toEqual(["Headquarters", "Service Depot"]);
    expect(rows.map((r) => r.kind)).toEqual(["location", "location"]);
  });

  it("lists a node's children: locations first, then the systems placed directly in it", () => {
    const rows = rowsFor(view, uuidFor("west"));
    expect(rows.map((r) => r.label)).toEqual(["Level 2", "Media Lab", "Storage"]);
  });

  it("collapses a location with one system and no sub-locations into that system's row", () => {
    const rows = rowsFor(view, uuidFor("l2"));
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe("system");
    expect(rows[0].label).toBe("Huddle");
    expect(rows[0].kind === "system" && rows[0].inLocation?.label).toBe("Huddle Room");
  });

  it("keeps a room with two systems as a node, and lists both systems in its column", () => {
    const west = rowsFor(view, uuidFor("west")).find((r) => r.label === "Media Lab")!;
    expect(west.kind).toBe("location");
    expect(rowsFor(view, uuidFor("media-lab")).map((r) => r.label)).toEqual(["Classroom", "Classroom 2"]);
  });

  it("marks a location with nothing under it as empty, still a row", () => {
    const storage = rowsFor(view, uuidFor("west")).find((r) => r.label === "Storage")!;
    expect(storage.kind === "location" && storage.systems).toBe(0);
    expect(storage.verdict).toBeNull();
  });
});

describe("subtreeVerdict", () => {
  it("rolls up the worst system verdict under a location, at any depth", () => {
    expect(subtreeVerdict(view, uuidFor("hq"))).toBe("degraded");
    expect(subtreeVerdict(view, uuidFor("l2"))).toBe("healthy");
    expect(subtreeVerdict(view, uuidFor("storage"))).toBeNull();
  });
});

describe("columnsFor", () => {
  it("builds one column per path step, headed by the node whose children it lists", () => {
    const cols = columnsFor(view, [uuidFor("hq"), uuidFor("west")]);
    expect(cols.map((c) => c.header)).toEqual(["Locations", "Headquarters", "West Building"]);
    expect(cols[2].rows.map((r) => r.label)).toEqual(["Level 2", "Media Lab", "Storage"]);
  });

  it("stops at a displaced or empty node: nothing deeper to list", () => {
    const cols = columnsFor(view, [uuidFor("hq"), uuidFor("west"), uuidFor("storage")]);
    expect(cols.map((c) => c.header)).toEqual(["Locations", "Headquarters", "West Building"]);
  });
});

describe("pathLabel and searchTree", () => {
  it("spells a location's path root-first", () => {
    expect(pathLabel(view, uuidFor("huddle-room"))).toBe("Headquarters / West Building / Level 2 / Huddle Room");
  });

  it("matches by label, name, or path fragment, systems before locations, with the path on every hit", () => {
    const hits = searchTree(view, "level 2");
    expect(hits[0].kind).toBe("system");
    expect(hits[0].label).toBe("Huddle");
    expect(hits[0].path).toBe("Headquarters / West Building / Level 2 / Huddle Room");
    expect(hits.some((h) => h.kind === "location" && h.label === "Level 2")).toBe(true);
    expect(searchTree(view, "classroom").map((h) => h.label)).toEqual(["Classroom", "Classroom 2"]);
    expect(searchTree(view, "")).toEqual([]);
  });
});
