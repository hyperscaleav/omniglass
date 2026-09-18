import { describe, expect, it } from "vitest";
import { componentTileSpec, fleetTileSpec, fleetTiles, locationTileSpec, systemMarks, systemTileSpec } from "./fleet_tiles";
import type { FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The fleet zoom's summary tiles and its system-grain marks (design option B,
// ruled 2026-08-18): one round mark per SYSTEM at the fleet zoom, coloured by
// system verdict, banded per root, worst first; the tiles carry what the rail
// carried, over SYSTEMS at this zoom. Pure, verdicts never computed.

const loc = (h: string, name: string, label: string, type: string, parent: string, verdict: string) => ({
  id: uuidFor(h), name, label, location_type: type, location_type_id: uuidFor(`ftt-${type}`), parent: parent ? uuidFor(parent) : "", verdict,
});
const sys = (h: string, label: string, loc: string, verdict: string, n = 2) => ({
  id: uuidFor(h), name: h, label, location: uuidFor(loc), verdict,
  dots: Array.from({ length: n }, (_, i) => ({ component: uuidFor(`${h}-c${i}`), name: `c${i}`, verdict: "healthy", primary: true, shared: false })),
});

const view: FleetView = {
  locations: [
    loc("ft-hq", "hq", "Headquarters", "campus", "", "outage"),
    loc("ft-b1", "b1", "Building 1", "building", "ft-hq", "outage"),
    loc("ft-r1", "r1", "Room 1", "room", "ft-b1", "outage"),
    loc("ft-r2", "r2", "Room 2", "room", "ft-b1", "healthy"),
    loc("ft-depot", "depot", "Depot", "building", "", "healthy"),
    loc("ft-bay", "bay-1", "Bay 1", "room", "ft-depot", "healthy"),
    loc("ft-empty", "bay-2", "Bay 2", "room", "ft-depot", "healthy"),
  ],
  systems: [
    sys("ft-s1", "S1", "ft-r1", "outage"),
    sys("ft-s2", "S2", "ft-r2", "healthy"),
    sys("ft-s3", "S3", "ft-r1", "incomplete"),
    sys("ft-s4", "S4", "ft-bay", "healthy", 3),
  ],
} as unknown as FleetView;

describe("fleetTiles", () => {
  const t = fleetTiles(view);
  it("counts systems and components (a shared component once) and roots", () => {
    expect(t.systems).toBe(4);
    expect(t.components).toBe(9);
    expect(t.roots).toBe(2);
  });
  it("counts what needs attention by verdict, over systems", () => {
    expect(t.attention).toEqual({ outage: 1, degraded: 0, incomplete: 1, total: 2 });
  });
  it("counts the gaps", () => {
    expect(t.gaps).toBe(1);
  });
  it("gives the health bar over systems, not components", () => {
    expect(t.ratio).toEqual({ healthy: 2, incomplete: 1, degraded: 0, outage: 1, total: 4 });
  });
  it("states leaf depth as a range", () => {
    expect(t.depth).toEqual({ min: 2, max: 3 });
  });
});

describe("systemMarks", () => {
  it("makes one cluster of ONE dot per system, the dot's verdict the system's, worst first within a band", () => {
    const bands = systemMarks(view);
    const hq = bands.find((b) => b.key === uuidFor("ft-hq"))!;
    expect(hq.clusters.map((c) => c.dots.length)).toEqual([1, 1, 1]);
    expect(hq.clusters.map((c) => c.dots[0].verdict)).toEqual(["outage", "incomplete", "healthy"]);
    // The dot IS the system: it carries the system id so a click opens it.
    expect(hq.clusters[0].dots[0].componentId).toBe(uuidFor("ft-s1"));
    expect(hq.clusters[0].systemId).toBe(uuidFor("ft-s1"));
  });
  it("keeps the band's own counts and recorded verdict", () => {
    const hq = systemMarks(view).find((b) => b.key === uuidFor("ft-hq"))!;
    expect(hq.systemCount).toBe(3);
    expect(hq.componentCount).toBe(6);
    expect(hq.recordedVerdict).toBe("outage");
  });
  it("filters by verdict when asked", () => {
    const only = systemMarks(view, { verdicts: new Set(["outage", "incomplete"]) });
    const hq = only.find((b) => b.key === uuidFor("ft-hq"))!;
    expect(hq.clusters.map((c) => c.dots[0].verdict)).toEqual(["outage", "incomplete"]);
    // A root left with nothing after the filter still appears, empty, so
    // the operator sees it was filtered rather than missing.
    const depot = only.find((b) => b.key === uuidFor("ft-depot"))!;
    expect(depot.clusters).toEqual([]);
  });
});

// The summary reflects the page it is on (#795 review): each scope builds its
// own TileSpec, so a system's rail talks about ITS components, never the
// whole fleet's numbers.
describe("the scoped tile specs", () => {
  it("the fleet spec carries the fleet-wide numbers under the systems subject", () => {
    const spec = fleetTileSpec(view);
    expect(spec.subject).toBe("systems");
    expect(spec.ratio.total).toBe(4);
    expect(spec.counts.map((c) => c.key)).toEqual(["gaps", "components", "roots"]);
  });

  it("a location's spec counts only its own subtree", () => {
    const spec = locationTileSpec(view, uuidFor("ft-b1"));
    expect(spec.subject).toBe("systems");
    // b1 holds r1 (S1 outage, S3 incomplete) and r2 (S2 healthy); the depot's
    // S4 never appears.
    expect(spec.ratio.total).toBe(3);
    expect(spec.attention.total).toBe(2);
    expect(spec.counts.find((c) => c.key === "components")!.value).toBe(6);
    expect(spec.counts.find((c) => c.key === "children")!.value).toBe(2);
  });

  it("a system's spec talks about its components: mix, attention, slots, alarms", () => {
    const health = {
      verdict: "degraded",
      roles: [
        { name: "r", label: "R", quorum: 2, satisfying: 1, short: 1, spare: 0, impact: "degraded", impaired: true, active: true,
          assigned_to: ["c0", "c1"], down: ["c1"],
          alarms: [{ id: "a1", component: uuidFor("ft-s1-c1"), severity: "critical", message: "m", raised_at: "2026-08-20T00:00:00Z" }] },
      ],
      systems: [], transitions: [],
    };
    const withDown = {
      ...view,
      systems: [{ ...((view.systems ?? [])[0] as object), dots: [
        { component: uuidFor("ft-s1-c0"), name: "c0", verdict: "healthy", primary: true, shared: false },
        { component: uuidFor("ft-s1-c1"), name: "c1", verdict: "outage", primary: true, shared: true },
      ] }],
    };
    const spec = systemTileSpec(withDown as never, health as never, uuidFor("ft-s1"));
    expect(spec.subject).toBe("components");
    expect(spec.ratio.total).toBe(2);
    expect(spec.ratio.outage).toBe(1);
    expect(spec.attention.total).toBe(1);
    expect(spec.counts.find((c) => c.key === "slots")!.value).toBe("1 of 2");
    expect(spec.counts.find((c) => c.key === "alarms")!.value).toBe(1);
    expect(spec.counts.find((c) => c.key === "shared")!.value).toBe(1);
  });

  it("a component's spec is its own story: state, memberships, alarms, interfaces", () => {
    const spec = componentTileSpec(view, uuidFor("ft-s4-c0"), 2, 1);
    expect(spec.subject).toBe("component");
    expect(spec.ratio.total).toBe(1);
    expect(spec.ratio.healthy).toBe(1);
    expect(spec.counts.find((c) => c.key === "systems")!.value).toBe(1);
    expect(spec.counts.find((c) => c.key === "alarms")!.value).toBe(2);
    expect(spec.counts.find((c) => c.key === "endpoints")!.value).toBe(1);
  });
});
