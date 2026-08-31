import { describe, expect, it } from "vitest";
import { aboveCutSystems, cutNodesFor, cutTypeFor, isLeafType, typeTiers, unplacedSystems } from "./place_cut";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The place cut (#837): which level of a root's tree becomes a card. Never a
// depth counted from the root, because location_type and allowed_parent_types
// make depth a customer's fact: one estate roots at a campus holding several
// buildings, the next roots at a building, and branches inside one tree
// disagree with each other. The cut is chosen per root from the types that
// root actually contains, and the tier order is derived from the tree rather
// than from a list of type names the renderer would have to know.

const loc = (handle: string, name: string, label: string, type: string, parent: string) => ({
  id: uuidFor(handle),
  name,
  label,
  location_type: type,
  location_type_id: uuidFor(`t-${type}`),
  parent: parent ? uuidFor(parent) : "",
  verdict: "healthy",
});
const sys = (handle: string, name: string, label: string, location: string) => ({
  id: uuidFor(handle),
  name,
  label,
  location: uuidFor(location),
  verdict: "healthy",
  dots: [],
});

// Three roots, three shapes, on purpose:
//   hq     campus > building > floor > room, and a room straight under a
//          building, so one branch is a level shallower than its sibling;
//   depot  building > room, no container level under it at all;
//   annex  campus > building > room, with only one building.
const view: FleetView = {
  locations: [
    loc("hq", "hq", "Headquarters", "campus", ""),
    loc("west", "west", "West Building", "building", "hq"),
    loc("l2", "level-2", "Level 2", "floor", "west"),
    loc("huddle", "huddle-room", "Huddle Room", "room", "l2"),
    loc("media-lab", "media-lab", "Media Lab", "room", "west"),
    loc("east", "east", "East Building", "building", "hq"),
    loc("l1", "level-1", "Level 1", "floor", "east"),
    loc("lab", "lab", "Lab", "room", "l1"),

    loc("depot", "depot", "Service Depot", "building", ""),
    loc("bay-1", "bay-1", "Bay 1", "room", "depot"),
    loc("bay-2", "bay-2", "Bay 2", "room", "depot"),

    loc("annex", "annex", "North Annex", "campus", ""),
    loc("north", "north", "North Building", "building", "annex"),
    loc("store", "store", "Store Room", "room", "north"),
  ],
  systems: [
    sys("s-huddle", "huddle", "Huddle", "huddle"),
    sys("s-media", "media", "Media", "media-lab"),
    sys("s-lab", "lab-av", "Lab AV", "lab"),
    sys("s-bay1", "bay-1-av", "Bay 1 AV", "bay-1"),
    sys("s-bay2", "bay-2-av", "Bay 2 AV", "bay-2"),
    sys("s-store", "store-av", "Store AV", "store"),
    // Attached to the campus itself: belongs to no building and no room.
    sys("s-paging", "paging", "Campus Paging", "hq"),
  ],
} as unknown as FleetView;

describe("typeTiers", () => {
  it("orders types by the shallowest depth each is observed at, not by name", () => {
    const tiers = typeTiers(view);
    expect(tiers.get("campus")).toBe(0);
    expect(tiers.get("building")).toBe(0); // depot is a root building
    expect(tiers.get("floor")).toBe(2);
    // depot is a root building holding rooms directly, so a room is observed
    // one level down even though most rooms sit three levels down.
    expect(tiers.get("room")).toBe(1);
  });

  it("knows a leaf type from one that contains something", () => {
    expect(isLeafType(view, "room")).toBe(true);
    expect(isLeafType(view, "building")).toBe(false);
    expect(isLeafType(view, "campus")).toBe(false);
  });
});

describe("cutTypeFor", () => {
  it("cuts at the shallowest container type the root has at least two of", () => {
    expect(cutTypeFor(view, uuidFor("hq"))).toBe("building");
  });

  it("falls back to the root itself when no container level has two", () => {
    // annex holds exactly one building, so cutting there would make one card
    // that is just the root wearing another name.
    expect(cutTypeFor(view, uuidFor("annex"))).toBe("campus");
  });

  it("falls back to the root when the only level below it is a leaf type", () => {
    // depot holds rooms directly. Rooms are where systems live, not cards.
    expect(cutTypeFor(view, uuidFor("depot"))).toBe("building");
  });
});

describe("cutNodesFor", () => {
  it("returns the nodes at the cut, and only those with systems beneath them", () => {
    const nodes = cutNodesFor(view, uuidFor("hq"));
    expect(nodes.map((n) => n.label)).toEqual(["East Building", "West Building"]);
  });

  it("returns the root itself when the root is its own cut", () => {
    expect(cutNodesFor(view, uuidFor("depot")).map((n) => n.label)).toEqual(["Service Depot"]);
    expect(cutNodesFor(view, uuidFor("annex")).map((n) => n.label)).toEqual(["North Annex"]);
  });

  it("never returns a node twice, whatever the branch depths", () => {
    const ids = cutNodesFor(view, uuidFor("hq")).map((n) => n.id);
    expect(new Set(ids).size).toBe(ids.length);
  });
});

describe("aboveCutSystems", () => {
  it("returns the systems attached at or above the cut, which belong to no card", () => {
    const above = aboveCutSystems(view, uuidFor("hq"));
    expect(above.map((s) => s.label)).toEqual(["Campus Paging"]);
  });

  it("counts such a system exactly once, and never inside a cut node", () => {
    const above = aboveCutSystems(view, uuidFor("hq")).map((s) => s.id);
    expect(above).toHaveLength(1);
    const inCards = cutNodesFor(view, uuidFor("hq")).flatMap((n) =>
      (view.systems ?? []).filter((s) => s.location === n.id).map((s) => s.id),
    );
    expect(inCards).not.toContain(above[0]);
  });

  it("is empty when the root is its own cut, since nothing sits above it", () => {
    expect(aboveCutSystems(view, uuidFor("depot"))).toEqual([]);
  });
});

describe("the cut is a function of the tree alone", () => {
  it("does not depend on the order locations arrive in", () => {
    const shuffled = {
      ...view,
      locations: [...(view.locations ?? [])].reverse(),
      systems: [...(view.systems ?? [])].reverse(),
    } as FleetView;
    expect(cutTypeFor(shuffled, uuidFor("hq"))).toBe(cutTypeFor(view, uuidFor("hq")));
    expect(cutNodesFor(shuffled, uuidFor("hq")).map((n) => n.label)).toEqual(
      cutNodesFor(view, uuidFor("hq")).map((n) => n.label),
    );
  });

  it("terminates on a parent cycle rather than hanging", () => {
    const cyclic = {
      locations: [
        { ...loc("a", "a", "A", "campus", "b") },
        { ...loc("b", "b", "B", "building", "a") },
      ],
      systems: [],
    } as unknown as FleetView;
    expect(() => cutTypeFor(cyclic, uuidFor("a"))).not.toThrow();
  });
});

describe("unplacedSystems", () => {
  // The fleet view scopes locations and systems through two separate scope
  // sets, so a principal whose system:read scope is wider than its
  // location:read scope receives systems whose location is not in the payload.
  // Walking from the roots alone would drop them silently.
  it("returns systems whose location the caller cannot read", () => {
    const partial = {
      locations: [loc("depot", "depot", "Service Depot", "building", ""), loc("bay-1", "bay-1", "Bay 1", "room", "depot")],
      systems: [
        sys("s-bay1", "bay-1-av", "Bay 1 AV", "bay-1"),
        // its location is real, but not one this caller may read
        sys("s-hidden", "hidden", "Hidden AV", "huddle"),
      ],
    } as unknown as FleetView;
    expect(unplacedSystems(partial).map((s) => s.label)).toEqual(["Hidden AV"]);
  });

  it("returns a system placed nowhere at all", () => {
    const nowhere = {
      locations: [loc("depot", "depot", "Service Depot", "building", "")],
      systems: [{ id: uuidFor("s-svc"), name: "svc", label: "Booking Service", verdict: "healthy", dots: [] }],
    } as unknown as FleetView;
    expect(unplacedSystems(nowhere).map((s) => s.label)).toEqual(["Booking Service"]);
  });

  it("is empty when every system's location is readable", () => {
    expect(unplacedSystems(view)).toEqual([]);
  });

  it("does not double-count a system already above a cut", () => {
    const above = aboveCutSystems(view, uuidFor("hq")).map((s) => s.id);
    const unplaced = unplacedSystems(view).map((s) => s.id);
    expect(unplaced.filter((id) => above.includes(id))).toEqual([]);
  });
});
