import { describe, expect, it } from "vitest";
import {
  attentionOf,
  countsLine,
  countsOf,
  fieldFor,
  insideOf,
  resolveNode,
  roomsInView,
  sectionsFor,
  unplacedFor,
  type ExploreOptions,
} from "./explore_view";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The explorer's view model (#839): the fleet view plus the controls, turned
// into what a renderer draws. Everything an operator could dispute lives here,
// so it is testable without a DOM and the four renderers cannot disagree.

const loc = (handle: string, label: string, type: string, parent: string) => ({
  id: uuidFor(handle),
  name: handle,
  label,
  location_type: type,
  location_type_id: uuidFor(`t-${type}`),
  parent: parent ? uuidFor(parent) : "",
  verdict: "healthy",
});
const sys = (handle: string, label: string, location: string, verdict: string) => ({
  id: uuidFor(handle),
  name: handle,
  label,
  location: location ? uuidFor(location) : "",
  verdict,
  dots: [],
});

const view: FleetView = {
  locations: [
    loc("hq", "Headquarters", "campus", ""),
    loc("west", "West Building", "building", "hq"),
    loc("l2", "Level 2", "floor", "west"),
    loc("huddle", "Huddle Room", "room", "l2"),
    loc("media", "Media Lab", "room", "west"),
    loc("east", "East Building", "building", "hq"),
    loc("l1", "Level 1", "floor", "east"),
    loc("lab", "Lab", "room", "l1"),
    loc("depot", "Service Depot", "building", ""),
    loc("bay1", "Bay 1", "room", "depot"),
    loc("bay2", "Bay 2", "room", "depot"),
  ],
  systems: [
    sys("s-huddle", "Huddle AV", "huddle", "healthy"),
    sys("s-media", "Media AV", "media", "degraded"),
    sys("s-lab", "Lab AV", "lab", "outage"),
    sys("s-bay1", "Bay 1 AV", "bay1", "healthy"),
    sys("s-bay2", "Bay 2 AV", "bay2", "healthy"),
    sys("s-paging", "Campus Paging", "hq", "healthy"),
    sys("s-orphan", "Orphan AV", "", "healthy"),
  ],
} as unknown as FleetView;

const all: ExploreOptions = { attentionOnly: false, sort: "worst" };
const attention: ExploreOptions = { attentionOnly: true, sort: "worst" };

describe("sectionsFor", () => {
  it("makes one section per root, each cut at its own level", () => {
    const s = sectionsFor(view, all);
    expect(s.map((x) => x.label)).toEqual(["Headquarters", "Service Depot"]);
    expect(s[0].cutType).toBe("building");
    expect(s[0].cards.map((c) => c.label)).toEqual(["East Building", "West Building"]);
    // depot holds only rooms, so those rooms are the cut: the deepest level is
    // the right one when it is the only one there is.
    expect(s[1].cutType).toBe("room");
    expect(s[1].cards.map((c) => c.label)).toEqual(["Bay 1", "Bay 2"]);
  });

  it("names each card's own type, so a mixed fleet reads as mixed", () => {
    const s = sectionsFor(view, all);
    expect(s[0].cards.map((c) => c.type)).toEqual(["building", "building"]);
    expect(s[1].cards.map((c) => c.type)).toEqual(["room", "room"]);
  });

  it("puts a system attached above the cut in the section, not in a card", () => {
    const hq = sectionsFor(view, all)[0];
    expect(hq.above.map((i) => i.label)).toEqual(["Campus Paging"]);
    const inCards = hq.cards.flatMap((c) => c.field.items.map((i) => i.label));
    expect(inCards).not.toContain("Campus Paging");
  });

  it("counts every system under a root once, including the one above the cut", () => {
    const hq = sectionsFor(view, all)[0];
    expect(hq.systems).toBe(4); // huddle, media, lab, paging
  });

  it("keeps an empty cut node as a card, so an unfinished tree stays visible", () => {
    const withEmpty = {
      ...view,
      locations: [...(view.locations ?? []), loc("north", "North Building", "building", "hq")],
    } as FleetView;
    const hq = sectionsFor(withEmpty, all)[0];
    expect(hq.cards.map((c) => c.label)).toEqual(["East Building", "North Building", "West Building"]);
    const north = hq.cards.find((c) => c.label === "North Building")!;
    expect(north.systems).toBe(0);
    expect(north.field.items).toEqual([]);
  });

  it("drops an empty card when the operator is filtering", () => {
    const withEmpty = {
      ...view,
      locations: [...(view.locations ?? []), loc("north", "North Building", "building", "hq")],
    } as FleetView;
    const hq = sectionsFor(withEmpty, attention)[0];
    expect(hq.cards.map((c) => c.label)).not.toContain("North Building");
  });

  it("drops a section with nothing left after the filter", () => {
    const s = sectionsFor(view, attention);
    expect(s.map((x) => x.label)).toEqual(["Headquarters"]);
    expect(s[0].cards.map((c) => c.label)).toEqual(["East Building", "West Building"]);
  });
});

describe("fieldFor", () => {
  it("nests a card's dots by the tree, carrying each node's height", () => {
    const field = fieldFor(view, uuidFor("west"), all);
    expect(field).not.toBeNull();
    // west holds Level 2 (a floor holding a room) and Media Lab (a room)
    expect(field!.height).toBe(2);
    const heights = field!.children.map((c) => c.height).sort();
    expect(heights).toEqual([0, 1]);
  });

  it("drops an empty branch rather than drawing an empty box", () => {
    const empty = { ...view, systems: [] } as FleetView;
    expect(fieldFor(empty, uuidFor("west"), all)).toBeNull();
  });

  it("orders worst first, then by label", () => {
    const field = fieldFor(view, uuidFor("east"), all);
    expect(field!.children[0].children[0].items.map((i) => i.label)).toEqual(["Lab AV"]);
  });
});

describe("insideOf", () => {
  it("cards a drilled node's children with the same shapes as the fleet level", () => {
    const inside = insideOf(view, uuidFor("west"), all);
    expect(inside!.cards.map((c) => c.label)).toEqual(["Level 2", "Media Lab"]);
  });

  it("puts a node's own systems above its children", () => {
    const inside = insideOf(view, uuidFor("hq"), all);
    expect(inside!.above.map((i) => i.label)).toEqual(["Campus Paging"]);
    expect(inside!.cards.map((c) => c.label)).toEqual(["East Building", "West Building"]);
  });

  it("returns a section for an existing node even when it holds nothing", () => {
    // A campus created a moment ago has no children and no systems. Returning
    // null there leaves nowhere to stand and no way to create the first thing.
    const empty = insideOf(view, uuidFor("bay1"), attention);
    expect(empty).not.toBeNull();
    expect(empty!.cards).toEqual([]);
    expect(empty!.above).toEqual([]);
    expect(empty!.label).toBe("Bay 1");
  });

  it("returns null only when the node does not exist", () => {
    expect(insideOf(view, uuidFor("nope"), all)).toBeNull();
  });
});

describe("unplacedFor", () => {
  it("surfaces a system placed nowhere, which no walk from the roots would find", () => {
    expect(unplacedFor(view, all).map((i) => i.label)).toEqual(["Orphan AV"]);
  });
});

describe("counts", () => {
  it("treats incomplete as not needing attention, since it is a commissioning gap", () => {
    const c = countsOf([{ verdict: "incomplete" }, { verdict: "degraded" }, { verdict: "outage" }]);
    expect(attentionOf(c)).toBe(2);
  });

  it("writes one line, zeros left out, singular reading as a fact", () => {
    expect(countsLine(countsOf([{ verdict: "healthy" }]))).toBe("1 system");
    expect(countsLine(countsOf([{ verdict: "healthy" }, { verdict: "outage" }]))).toBe("2 systems · 1 in outage");
  });
});

describe("roomsInView", () => {
  it("counts the leaf locations in front of the operator, not the fleet's total", () => {
    expect(roomsInView(view, [uuidFor("hq"), uuidFor("depot")])).toBe(5);
    expect(roomsInView(view, [uuidFor("west")])).toBe(2);
  });
});

describe("resolveNode", () => {
  // ADR-0062: the uuid is the address, but a name-shaped one resolves when it
  // is unique. #831 built that rule in pathForNode and the drill must keep it,
  // or a documented link like ?node=huddle lands on an empty page.
  it("takes a location's uuid", () => {
    expect(resolveNode(view, uuidFor("west"))).toBe(uuidFor("west"));
  });

  it("takes a location's unique name", () => {
    expect(resolveNode(view, "west")).toBe(uuidFor("west"));
  });

  it("drills to where a system is when given the system", () => {
    expect(resolveNode(view, "s-lab")).toBe(uuidFor("lab"));
  });

  it("resolves nothing rather than guessing at an unknown address", () => {
    expect(resolveNode(view, "nope")).toBeNull();
    expect(resolveNode(view, "")).toBeNull();
  });
});
