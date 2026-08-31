import { describe, expect, it } from "vitest";
import { pathForNode, pathLabel, searchTree, subtreeVerdict } from "./explore";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// What survives of the original explore core (#826). The Miller-column
// builders retired with the columns themselves; what a card renderer still
// needs from here is the roll-up, the path an address resolves through, and
// search, because a name repeats across rooms and only the path disambiguates.

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

describe("subtreeVerdict", () => {
  it("rolls up the worst system verdict under a location, at any depth", () => {
    expect(subtreeVerdict(view, uuidFor("hq"))).toBe("degraded");
    expect(subtreeVerdict(view, uuidFor("l2"))).toBe("healthy");
    expect(subtreeVerdict(view, uuidFor("storage"))).toBeNull();
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

describe("pathForNode", () => {
  it("resolves a uuid to its path, minus a location that collapses into its system", () => {
    expect(pathForNode(view, uuidFor("s-huddle"))).toEqual({ path: [uuidFor("hq"), uuidFor("west"), uuidFor("l2")], selected: uuidFor("s-huddle") });
    expect(pathForNode(view, uuidFor("west"))).toEqual({ path: [uuidFor("hq"), uuidFor("west")], selected: uuidFor("west") });
  });

  it("resolves a name-shaped address when it is unique, systems before locations, and refuses an ambiguous or unknown one", () => {
    expect(pathForNode(view, "huddle")?.selected).toBe(uuidFor("s-huddle"));
    expect(pathForNode(view, "west")?.selected).toBe(uuidFor("west"));
    expect(pathForNode(view, "no-such-thing")).toBeNull();
  });
});
