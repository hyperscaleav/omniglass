import { describe, expect, it } from "vitest";
import { railModel } from "./zoom_rail";
import type { FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The right rail's model, one shape for every zoom (#630 design fidelity):
// a headline, a health ratio over the components in scope, a topology
// sentence with type chips, a worst-first attention list, and a gaps
// footer. Pure, so the ratio arithmetic and the worst-first sort are pinned
// with no DOM; verdict words come from the server and are never computed.

const loc = (h: string, name: string, label: string, type: string, parent: string, verdict: string) => ({
  id: uuidFor(h), name, label, location_type: type, location_type_id: uuidFor(`zrt-${type}`), parent: parent ? uuidFor(parent) : "", verdict,
});
const dot = (h: string, verdict: string) => ({ component: uuidFor(h), name: h, verdict, primary: true, shared: false });

const view: FleetView = {
  locations: [
    loc("zr-hq", "hq", "Headquarters", "campus", "", "outage"),
    loc("zr-b1", "west", "West", "building", "zr-hq", "outage"),
    loc("zr-r1", "r1", "Room 1", "room", "zr-b1", "outage"),
    loc("zr-r2", "r2", "Room 2", "room", "zr-b1", "healthy"),
    loc("zr-depot", "depot", "Depot", "campus", "", "healthy"),
    loc("zr-bay", "bay-1", "Bay 1", "room", "zr-depot", "healthy"),
    // A leaf with no system: a hole.
    loc("zr-empty", "bay-2", "Bay 2", "room", "zr-depot", "healthy"),
  ],
  systems: [
    { id: uuidFor("zr-s1"), name: "s1", label: "S1", location: uuidFor("zr-r1"), verdict: "outage", dots: [dot("c1", "outage"), dot("c2", "healthy"), dot("c3", "degraded")] },
    { id: uuidFor("zr-s2"), name: "s2", label: "S2", location: uuidFor("zr-r2"), verdict: "healthy", dots: [dot("c4", "healthy")] },
    { id: uuidFor("zr-s3"), name: "s3", label: "S3", location: uuidFor("zr-bay"), verdict: "incomplete", dots: [dot("c5", "incomplete")] },
  ],
} as unknown as FleetView;

describe("railModel at the fleet zoom", () => {
  const m = railModel({ zoom: "fleet" }, view);
  it("headlines the totals and a health ratio over every component", () => {
    expect(m.kicker).toBe("Fleet");
    expect(m.sub).toBe("3 systems · 5 components · 2 roots");
    // 5 dots: 1 outage, 1 degraded, 1 incomplete, 2 healthy.
    expect(m.ratio).toEqual({ healthy: 2, incomplete: 1, degraded: 1, outage: 1, total: 5 });
  });
  it("lists the roots needing attention, worst first, and leaves healthy roots off", () => {
    expect(m.listTitle).toBe("Roots needing attention");
    expect(m.list.map((r) => [r.label, r.verdict])).toEqual([["Headquarters", "outage"]]);
  });
  it("names the location types present shallowest-first, and how deep the leaves sit", () => {
    // Shallowest first: campus (depth 1) leads; building and room both first
    // appear at depth 2 (west under hq, the bays under depot) and the tie
    // breaks toward the more frequent one at that depth (two bays, one
    // building). Every type present is listed exactly once.
    expect(m.types[0]).toBe("campus");
    expect([...m.types].sort()).toEqual(["building", "campus", "room"]);
    expect(m.topology).toContain("2 to 3 levels");
  });
  it("counts the holes for the footer", () => {
    expect(m.gaps).toBe("1 location has no system");
  });
});

describe("railModel at a location zoom", () => {
  const m = railModel({ zoom: "location", locationId: uuidFor("zr-hq") }, view);
  it("scopes the ratio and the list to the subtree, worst first", () => {
    expect(m.kicker).toBe("campus");
    expect(m.title).toBe("Headquarters");
    expect(m.sub).toBe("2 systems · 4 components");
    expect(m.ratio.total).toBe(4);
    expect(m.list.map((r) => r.label)).toEqual(["S1"]);
  });
  it("states the path and the child types beneath", () => {
    expect(m.topology).toContain("campus");
    expect(m.types).toEqual(["building", "room"]);
  });
  it("counts only this subtree's holes", () => {
    expect(m.gaps).toBe("0 locations have no system");
  });
});

describe("railModel at a system zoom", () => {
  const m = railModel({ zoom: "system", systemId: uuidFor("zr-s1") }, view);
  it("scopes to the system's own components", () => {
    expect(m.kicker).toBe("System");
    expect(m.title).toBe("S1");
    expect(m.ratio.total).toBe(3);
    expect(m.topology).toContain("Headquarters");
  });
});
