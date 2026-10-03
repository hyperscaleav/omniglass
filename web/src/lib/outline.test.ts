import { describe, expect, it } from "vitest";
import { ancestorsOf, buildOutline, entriesOf, type OutlineInput, type PlaceNode } from "./outline";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The outline's model (#866): the place tree, each place wearing its system,
// the components under it, the counts, folding and filter entries. Pure, so
// everything an operator could dispute is tested here without a DOM.

const loc = (h: string, label: string, type: string, parent = "", verdict = "healthy") => ({
  id: uuidFor(h), name: h, label, location_type: type, location_type_id: uuidFor(`t-${type}`),
  parent: parent ? uuidFor(parent) : undefined, verdict,
});
const dot = (h: string, verdict = "healthy", shared = false) => ({ component: uuidFor(h), name: h, verdict, primary: true, shared });
const sys = (h: string, label: string, at: string, verdict: string, dots: ReturnType<typeof dot>[] = []) => ({
  id: uuidFor(h), name: h, label, location: at ? uuidFor(at) : undefined, verdict, dots,
});
const type = (name: string, label: string, parents: string[]) => ({ name, label, icon: name, allowed_parent_types: parents });

const TYPES = [
  type("campus", "Campus", ["root"]), type("building", "Building", ["root", "campus"]),
  type("floor", "Floor", ["building"]), type("room", "Room", ["floor", "building", "campus"]),
];

function input(view: Partial<FleetView>, over: Partial<OutlineInput> = {}): OutlineInput {
  return {
    view: { locations: [], systems: [], ...view } as FleetView,
    components: [],
    systems: [],
    types: TYPES,
    standardLabel: (h) => ({ "divisible-conference": "Divisible Conference", auditorium: "Auditorium" } as Record<string, string>)[h] ?? h,
    productLabel: (h) => h,
    ...over,
  };
}

const roots = (o: { roots: PlaceNode[] }) => o.roots.map((r) => r.chain.map((c) => c.label).join(" / "));
const child = (n: PlaceNode, label: string) => n.children.find((c) => c.chain.some((x) => x.label === label))!;

describe("the place tree", () => {
  it("nests places by parent and sorts every level by name", () => {
    const o = buildOutline(input({ locations: [loc("z", "Zeta", "campus"), loc("a", "Alpha", "campus"), loc("a2", "Beta", "building", "a"), loc("a1", "Annex", "building", "a")] }));
    expect(roots(o)).toEqual(["Alpha", "Zeta"]);
    expect(o.roots[0].children.map((c) => c.chain[0].label)).toEqual(["Annex", "Beta"]);
  });

  it("describes a hierarchy that shares no word with the shipped types in its own words", () => {
    const agri = [type("plot", "Plot", ["root"]), type("sector", "Sector", ["plot"]), type("coordinate", "Coordinate", ["sector"])];
    const o = buildOutline(input({
      locations: [loc("p", "North Plot", "plot"), loc("s1", "Sector One", "sector", "p"), loc("s2", "Sector Two", "sector", "p"), loc("c1", "Grid 1-1", "coordinate", "s1"), loc("c2", "Grid 1-2", "coordinate", "s1")],
    }, { types: agri }));
    const plot = o.roots[0];
    expect(plot.type).toBe("Plot");
    expect(plot.contents).toBe("2 sectors");
    expect(child(plot, "Sector One").contents).toBe("2 coordinates");
    expect(child(plot, "Sector Two").contents).toBe("Empty");
  });

  it("keeps an acronym type label as written when it counts it", () => {
    const o = buildOutline(input({ locations: [loc("p", "Plant", "campus"), loc("z1", "Z1", "zone", "p"), loc("z2", "Z2", "zone", "p")] }, {
      types: [...TYPES, type("zone", "HVAC Zone", ["campus"])],
    }));
    expect(o.roots[0].contents).toBe("2 HVAC Zones");
  });

  it("survives a parent cycle in the data", () => {
    expect(() => buildOutline(input({ locations: [loc("a", "A", "room", "b"), loc("b", "B", "room", "a")] }))).not.toThrow();
  });
});

describe("a place wears its system", () => {
  const view = {
    locations: [loc("hq", "Headquarters", "campus"), loc("w", "West", "building", "hq"), loc("e", "East", "building", "hq"), loc("ba", "Boardroom A", "room", "w"), loc("bb", "Boardroom B", "room", "w")],
    systems: [sys("s-ba", "Boardroom", "ba", "degraded", [dot("bar-1"), dot("mic-1", "outage")])],
  };
  const extra = {
    systems: [{ id: uuidFor("s-ba"), standard: "divisible-conference" }],
    components: [
      { id: uuidFor("bar-1"), name: "bar-1", label: "Video Bar", product: "Lyra Bar 80", system_count: 1, system_id: uuidFor("s-ba"), location_id: uuidFor("ba") },
      { id: uuidFor("mic-1"), name: "mic-1", label: "", product: "Lyra GN-12", system_count: 1, system_id: uuidFor("s-ba"), location_id: uuidFor("ba") },
    ],
  };

  it("puts the system's standard and health on the place's own row", () => {
    const ba = child(child(buildOutline(input(view, extra)).roots[0], "West"), "Boardroom A");
    expect(ba.systems.map((s) => s.standard)).toEqual(["Divisible Conference"]);
    expect(ba.health).toBe("degraded");
  });

  it("lists the system's components directly beneath the place, by name, with product and health", () => {
    const ba = child(child(buildOutline(input(view, extra)).roots[0], "West"), "Boardroom A");
    expect(ba.groups).toHaveLength(1);
    expect(ba.groups[0].components.map((c) => [c.name, c.label, c.product, c.health])).toEqual([
      ["bar-1", "Video Bar", "Lyra Bar 80", "healthy"],
      ["mic-1", "mic-1", "Lyra GN-12", "outage"],
    ]);
    expect(ba.contents).toBe("2 components");
  });

  it("gives each component its type's icon, resolved from its product", () => {
    const ba = child(child(buildOutline(input(view, { ...extra, componentIcon: (p) => (p === "Lyra Bar 80" ? "video" : "box") })).roots[0], "West"), "Boardroom A");
    expect(ba.groups[0].components.map((c) => c.icon)).toEqual(["video", "box"]);
  });

  it("counts every system beneath a place, inclusively, by health", () => {
    const hq = buildOutline(input(view, extra)).roots[0];
    expect(hq.lights).toEqual({ healthy: 0, incomplete: 0, degraded: 1, outage: 0 });
    expect(child(hq, "East").lights).toEqual({ healthy: 0, incomplete: 0, degraded: 0, outage: 0 });
  });

  it("reads Empty for a place that holds nothing", () => {
    expect(child(buildOutline(input(view, extra)).roots[0], "East").contents).toBe("Empty");
  });
});

describe("two systems, shared components, and components in no system", () => {
  const view = {
    locations: [loc("lab", "Media Lab", "room"), loc("b", "Boardroom B", "room")],
    systems: [
      sys("s-1", "Classroom", "lab", "healthy", [dot("cam-1"), dot("svc-1", "healthy", true)]),
      sys("s-2", "Classroom 2", "lab", "incomplete", [dot("disp-2", "incomplete")]),
      sys("s-b", "Boardroom", "b", "healthy", [dot("svc-1", "healthy", true)]),
    ],
  };
  const comps = [
    { id: uuidFor("loose"), name: "loose", label: "Spare Display", product: "Boreal", system_count: 0, location_id: uuidFor("lab") },
  ];

  it("groups a place's components under one header per system when it holds more than one", () => {
    const lab = buildOutline(input(view, { components: comps })).roots.find((r) => r.chain[0].label === "Media Lab")!;
    expect(lab.systems.map((s) => s.label)).toEqual(["Classroom", "Classroom 2"]);
    expect(lab.groups.map((g) => g.system?.label ?? null)).toEqual(["Classroom", "Classroom 2", null]);
    expect(lab.health).toBeNull();
    expect(lab.lights).toEqual({ healthy: 1, incomplete: 1, degraded: 0, outage: 0 });
  });

  it("lists a shared component under every system it serves, naming the others", () => {
    const o = buildOutline(input(view, { components: comps }));
    const svcInLab = o.roots.find((r) => r.chain[0].label === "Media Lab")!.groups[0].components.find((c) => c.name === "svc-1")!;
    const svcInB = o.roots.find((r) => r.chain[0].label === "Boardroom B")!.groups[0].components.find((c) => c.name === "svc-1")!;
    expect(svcInLab.also).toEqual(["Boardroom"]);
    expect(svcInB.also).toEqual(["Classroom"]);
  });

  it("puts a component in no system under its place, with no system header", () => {
    const lab = buildOutline(input(view, { components: comps })).roots.find((r) => r.chain[0].label === "Media Lab")!;
    expect(lab.groups[2].components.map((c) => c.label)).toEqual(["Spare Display"]);
    expect(lab.contents).toBe("4 components");
  });
});

describe("folding", () => {
  const view = {
    locations: [loc("hq", "Headquarters", "campus"), loc("w", "West Building", "building", "hq"), loc("l2", "Level 2", "floor", "w"), loc("l3", "Level 3", "floor", "w")],
  };

  it("folds a chain of places whose only child is another place into one row", () => {
    const hq = buildOutline(input(view)).roots[0];
    expect(hq.chain.map((c) => c.label)).toEqual(["Headquarters", "West Building"]);
    expect(hq.id).toBe(uuidFor("w"));
    expect(hq.type).toBe("Building");
    expect(hq.children.map((c) => c.chain[0].label)).toEqual(["Level 2", "Level 3"]);
  });

  it("counts a folded child by the place it starts at, which is what its parent holds", () => {
    // Level 2 holds one room and nothing else, so it folds into "Level 2 / Huddle".
    // West still holds two FLOORS, not a floor and a room.
    const o = buildOutline(input({
      locations: [loc("w", "West", "building"), loc("l2", "Level 2", "floor", "w"), loc("h", "Huddle", "room", "l2"), loc("l3", "Level 3", "floor", "w")],
    }));
    expect(o.roots[0].contents).toBe("2 floors");
    expect(child(o.roots[0], "Level 2").chain.map((c) => c.label)).toEqual(["Level 2", "Huddle"]);
  });

  it("never folds a place that holds a system or a component of its own", () => {
    const o = buildOutline(input({ ...view, systems: [sys("paging", "Paging", "hq", "healthy")] }));
    expect(o.roots[0].chain.map((c) => c.label)).toEqual(["Headquarters"]);
  });
});

describe("what you can and cannot see", () => {
  it("collects systems readable at a place you cannot read under their own node", () => {
    const o = buildOutline(input({ locations: [loc("a", "A", "room")], systems: [sys("s", "Hidden AV", "elsewhere", "healthy"), sys("n", "Nowhere AV", "", "outage")] }));
    expect(o.unplaced?.systems.map((s) => s.label)).toEqual(["Hidden AV", "Nowhere AV"]);
    expect(o.unplaced?.lights.outage).toBe(1);
    expect(o.systemCount).toBe(2);
  });
});

describe("things placed nowhere", () => {
  it("collects a component in no system and at no place you can read beside the unplaced systems", () => {
    const o = buildOutline(input({ locations: [loc("a", "A", "room")] }, {
      components: [
        { id: uuidFor("float"), name: "device-1", label: "Device 1", product: "generic", system_count: 0 },
        { id: uuidFor("lost"), name: "device-2", label: "", product: "generic", system_count: 0, location_id: uuidFor("elsewhere") },
      ],
    }));
    expect(o.unplaced?.chain[0].label).toBe("Placed nowhere you can see");
    expect(o.unplaced?.groups.flatMap((g) => g.components.map((c) => c.label))).toEqual(["Device 1", "device-2"]);
    expect(o.unplaced?.contents).toBe("2 components");
  });
});

describe("filter entries and reveal", () => {
  const view = {
    locations: [loc("hq", "Headquarters", "campus"), loc("w", "West", "building", "hq"), loc("e", "East", "building", "hq"), loc("ba", "Boardroom A", "room", "w"), loc("bb", "Boardroom B", "room", "w")],
    systems: [sys("s-ba", "Boardroom", "ba", "degraded", [dot("mic-1", "outage")])],
  };
  const over = { systems: [{ id: uuidFor("s-ba"), standard: "divisible-conference" }], components: [{ id: uuidFor("mic-1"), name: "mic-1", label: "", product: "Lyra GN-12", system_count: 1, system_id: uuidFor("s-ba"), location_id: uuidFor("ba") }] };

  it("gives every place and component an entry with its path and the facts a filter matches", () => {
    const es = entriesOf(buildOutline(input(view, over)));
    const ba = es.find((e) => e.id === uuidFor("ba"))!;
    expect(ba.path).toEqual(["Headquarters", "West"]);
    expect(ba.standard).toBe("Divisible Conference");
    expect(ba.verdict).toBe("degraded");
    expect(ba.search).toContain("Boardroom");
    const mic = es.find((e) => e.id === uuidFor("mic-1"))!;
    expect(mic.kind).toBe("component");
    expect(mic.path).toEqual(["Headquarters", "West", "Boardroom A"]);
    expect(mic.product).toBe("Lyra GN-12");
    expect(mic.verdict).toBe("outage");
  });

  it("carries each entry's effective tags, so a tag filter matches places and components alike", () => {
    const tags = new Map([[uuidFor("ba"), { environment: "staging" }], [uuidFor("mic-1"), { environment: "prod" }]]);
    const es = entriesOf(buildOutline(input(view, { ...over, tags })));
    expect(es.find((e) => e.id === uuidFor("ba"))!.tags).toEqual({ environment: "staging" });
    expect(es.find((e) => e.id === uuidFor("mic-1"))!.tags).toEqual({ environment: "prod" });
    expect(es.find((e) => e.id === uuidFor("hq"))!.tags).toEqual({});
  });

  it("names the places to expand to reveal a row, top first", () => {
    const o = buildOutline(input(view, over));
    expect(ancestorsOf(o, uuidFor("mic-1"))).toEqual([uuidFor("hq"), uuidFor("w"), uuidFor("ba")]);
    expect(ancestorsOf(o, uuidFor("ba"))).toEqual([uuidFor("hq"), uuidFor("w")]);
    expect(ancestorsOf(o, uuidFor("nope"))).toEqual([]);
    // A system is its place's own row, so only the places above it open.
    expect(ancestorsOf(o, uuidFor("s-ba"))).toEqual([uuidFor("hq"), uuidFor("w")]);
  });
});
