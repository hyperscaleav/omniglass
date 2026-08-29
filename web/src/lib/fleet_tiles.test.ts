import { describe, expect, it } from "vitest";
import { componentTileSpec, fleetTiles, locationTileSpec, systemTileSpec } from "./fleet_tiles";
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


// The summary reflects the page it is on (#795 review): each scope builds its
// own TileSpec, so a system's rail talks about ITS components, never the
// whole fleet's numbers.
describe("the scoped tile specs", () => {

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
    expect(spec.counts.find((c) => c.key === "interfaces")!.value).toBe(1);
  });
});

// The one counts line (#826 slice 3): what the summary rail said, as one
// sentence with the zero values left out, so a healthy room says nothing it
// does not need to.
import { countsLine } from "./fleet_tiles";
describe("countsLine", () => {
  it("leads with the mix total, adds need-attention only when non-zero, then the non-zero counts", () => {
    const spec = {
      subject: "systems",
      ratio: { healthy: 39, incomplete: 1, degraded: 1, outage: 0, total: 41 },
      attention: { outage: 0, degraded: 1, incomplete: 1, total: 2 },
      counts: [
        { key: "gaps", label: "gaps", value: 2 },
        { key: "components", label: "components", value: 206 },
        { key: "roots", label: "roots", value: 5 },
      ],
    };
    expect(countsLine(spec)).toEqual(["41 systems", "2 need attention", "2 gaps", "206 components", "5 roots"]);
  });

  it("drops zeros and empty strings, keeps a non-empty string count, and pluralises by the spec's own label", () => {
    const spec = {
      subject: "components",
      ratio: { healthy: 3, incomplete: 0, degraded: 0, outage: 0, total: 3 },
      attention: { outage: 0, degraded: 0, incomplete: 0, total: 0 },
      counts: [
        { key: "slots", label: "slots filled", value: "2 of 2" },
        { key: "alarms", label: "active alarms", value: 0 },
        { key: "shared", label: "shared", value: 0 },
        { key: "empty", label: "gaps", value: "" },
      ],
    };
    expect(countsLine(spec)).toEqual(["3 components", "2 of 2 slots filled"]);
  });
});
