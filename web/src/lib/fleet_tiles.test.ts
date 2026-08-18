import { describe, expect, it } from "vitest";
import { fleetTiles, systemMarks } from "./fleet_tiles";
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
