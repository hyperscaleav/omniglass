import { describe, it, expect } from "vitest";
import {
  ancestors,
  bandsOf,
  byRootLocation,
  estateTotals,
  locationIndex,
  locationsWithoutSystems,
  rootOf,
  toCluster,
  type EstateView,
  type Grouping,
} from "./estate";
import { uuidFor } from "./testids";

// The fixture is the estate the canvas has to survive: two roots of different
// depth, no level in common below the root, and one component shared between
// two systems in DIFFERENT roots, which is the case every count below is really
// testing.
// Ids are uuids post-ADR-0062, so fixtures mint them from a handle rather than
// putting the handle in the id slot: a fixture that fakes the shape lets a real
// join break ship green (registry-fixture-guard).
const loc = (handle: string, name: string, type: string, parent?: string) => ({
  id: uuidFor(handle),
  name,
  label: "",
  location_type: type,
  location_type_id: uuidFor(`type-${type}`),
  parent: parent ? uuidFor(parent) : "",
  verdict: "healthy",
});

const dot = (component: string, name: string, verdict: string, primary: boolean, shared: boolean) => ({
  component,
  name,
  verdict,
  primary,
  shared,
});

const view: EstateView = {
  locations: [
    loc("l-hq", "hq", "campus"),
    loc("l-b1", "hq-b1", "building", "l-hq"),
    loc("l-r1", "hq-r1", "room", "l-b1"),
    loc("l-r2", "hq-r2", "room", "l-b1"),
    loc("l-depot", "depot", "building"),
    loc("l-dr1", "depot-r1", "room", "l-depot"),
  ],
  systems: [
    {
      id: uuidFor("s-hq"),
      name: "hq-huddle",
      label: "HQ Huddle",
      location: uuidFor("l-r1"),
      verdict: "degraded",
      dots: [dot(uuidFor("c-bar"), "bar-1", "healthy", true, true), dot(uuidFor("c-mic"), "mic-1", "degraded", true, false)],
    },
    {
      id: uuidFor("s-depot"),
      name: "depot-huddle",
      label: "",
      location: uuidFor("l-dr1"),
      verdict: "outage",
      // The same physical box, a ghost here: owned by hq-huddle.
      dots: [dot(uuidFor("c-bar"), "bar-1", "healthy", false, true)],
    },
  ],
} as unknown as EstateView;

describe("rootOf", () => {
  it("walks an arbitrary-depth tree to its root", () => {
    const index = locationIndex(view);
    expect(rootOf(uuidFor("l-r1"), index)?.id).toBe(uuidFor("l-hq"));
    expect(rootOf(uuidFor("l-dr1"), index)?.id).toBe(uuidFor("l-depot"));
    // A root is its own root, and no level is required on the way: depot holds
    // a room directly, hq goes campus, building, room.
    expect(rootOf(uuidFor("l-hq"), index)?.id).toBe(uuidFor("l-hq"));
  });

  it("does not hang on a parent cycle", () => {
    const cyclic = {
      locations: [loc("cyc-a", "a", "room", "cyc-b"), loc("cyc-b", "b", "room", "cyc-a")],
      systems: [],
    } as unknown as EstateView;
    expect(() => rootOf(uuidFor("cyc-a"), locationIndex(cyclic))).not.toThrow();
  });

  it("is null for a system placed nowhere", () => {
    expect(rootOf(null, locationIndex(view))).toBeNull();
  });
});

describe("ancestors", () => {
  it("reads root first, which is the order the breadcrumb renders", () => {
    expect(ancestors(uuidFor("l-r1"), locationIndex(view)).map((l) => l.name)).toEqual(["hq", "hq-b1", "hq-r1"]);
  });
});

describe("toCluster", () => {
  it("carries the ring-and-ghost flags through to the dot", () => {
    const c = toCluster(view.systems![1]);
    expect(c.dots[0]).toMatchObject({ componentId: uuidFor("c-bar"), owned: false, shared: true });
  });

  it("labels a system by its display name, falling back to the name", () => {
    expect(toCluster(view.systems![0]).label).toBe("HQ Huddle");
    expect(toCluster(view.systems![1]).label).toBe("depot-huddle");
  });
});

describe("bandsOf", () => {
  it("gathers one band per root location", () => {
    const bands = bandsOf(view);
    expect(bands.map((b) => b.label)).toEqual(["depot", "hq"]);
  });

  it("rolls a band up worst-wins over its systems", () => {
    const bands = bandsOf(view);
    expect(bands.find((b) => b.label === "hq")?.verdict).toBe("degraded");
    expect(bands.find((b) => b.label === "depot")?.verdict).toBe("outage");
  });

  // The count a shared component makes wrong. bar-1 is one box in two systems
  // in two different roots; each band counts it once, and the estate total
  // counts it once overall.
  it("counts a component once per band, not once per dot", () => {
    const hq = bandsOf(view).find((b) => b.label === "hq")!;
    expect(hq.componentCount).toBe(2);
    expect(hq.systemCount).toBe(1);
  });

  it("carries the type as the band's sublabel, for its chip", () => {
    expect(bandsOf(view).find((b) => b.label === "hq")?.sublabel).toBe("campus");
  });

  // The seam. A second grouping must land the same dots in different rows with
  // no change to anything that renders them.
  it("regroups the same dots on a different axis", () => {
    const byVerdict: Grouping = {
      name: "verdict",
      bandFor: (s) => s.verdict ?? null,
      label: (k) => k,
      sublabel: () => "",
      order: (keys) => [...keys].sort(),
    };
    const bands = bandsOf(view, byVerdict);
    expect(bands.map((b) => b.label)).toEqual(["degraded", "outage"]);
    // Same dots, different rows: nothing was dropped or duplicated by regrouping.
    const before = bandsOf(view).flatMap((b) => b.clusters.flatMap((c) => c.dots.map((d) => d.componentId)));
    const after = bands.flatMap((b) => b.clusters.flatMap((c) => c.dots.map((d) => d.componentId)));
    expect(after.sort()).toEqual(before.sort());
  });

  it("drops a system its grouping cannot place, rather than inventing a band", () => {
    const orphan = {
      ...view,
      systems: [...view.systems!, { id: uuidFor("s-none"), name: "floating", label: "", location: "", verdict: "healthy", dots: [] }],
    } as unknown as EstateView;
    expect(bandsOf(orphan).length).toBe(2);
  });
});

describe("locationsWithoutSystems", () => {
  // A leaf holding nothing is the dashed hole the canvas draws. A branch is not
  // a hole: its rooms are where systems belong, and drawing one at every level
  // above them would bury the real gaps.
  it("names empty leaves and not their parents", () => {
    expect(locationsWithoutSystems(view).map((l) => l.name)).toEqual(["hq-r2"]);
  });

  // The system tier is scoped independently of the place tree, so a view can
  // arrive with locations and no systems because the caller may not read them.
  // Claiming every leaf is a hole there would tell a scoped operator their
  // commissioned estate is empty, and nothing here can tell that case from a
  // genuinely empty one, so it claims nothing.
  it("claims no holes when it cannot see the systems at all", () => {
    const noSystems = { ...view, systems: [] } as unknown as EstateView;
    expect(locationsWithoutSystems(noSystems)).toEqual([]);
  });
});

describe("estateTotals", () => {
  it("counts a shared component once across the whole estate", () => {
    expect(estateTotals(view)).toEqual({ systems: 2, components: 2, roots: 2 });
  });
});

describe("byRootLocation", () => {
  it("names itself, so a future picker has something to show", () => {
    expect(byRootLocation.name).toBe("location");
  });
});
