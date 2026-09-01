import { describe, expect, it } from "vitest";
import { matrixFor, NO_STANDARD } from "./matrix";
import { type FleetView } from "./fleet";
import { type ExploreOptions } from "./explore_view";
import { uuidFor } from "./testids";

// The matrix's pure core (#840): place against standard. Rows follow the same
// per-root cut the cards use, so the two faces cannot disagree about what a
// unit of the fleet is.

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
  location: uuidFor(location),
  verdict,
  dots: [],
});

const view: FleetView = {
  locations: [
    loc("hq", "Headquarters", "campus", ""),
    loc("west", "West Building", "building", "hq"),
    loc("east", "East Building", "building", "hq"),
    loc("depot", "Service Depot", "building", ""),
    loc("bay1", "Bay 1", "room", "depot"),
    loc("bay2", "Bay 2", "room", "depot"),
  ],
  systems: [
    sys("s-w1", "West One", "west", "healthy"),
    sys("s-w2", "West Two", "west", "outage"),
    sys("s-e1", "East One", "east", "degraded"),
    sys("s-b1", "Bay One", "bay1", "healthy"),
    sys("s-b2", "Bay Two", "bay2", "healthy"),
  ],
} as unknown as FleetView;

// The standard is not on the fleet wire, so the caller joins it in.
const standards: Record<string, string> = {
  [uuidFor("s-w1")]: "mr65",
  [uuidFor("s-w2")]: "mr65",
  [uuidFor("s-e1")]: "mr86",
  [uuidFor("s-b1")]: "ds55",
  // s-b2 conforms to nothing
};
const standardOf = (id: string) => standards[id];

const all: ExploreOptions = { sort: "worst" };
// The chrome filters the rows and hands down the survivors; here, the two that
// need attention.
const attention: ExploreOptions = { sort: "worst", include: (id) => id === uuidFor("s-w2") || id === uuidFor("s-e1") };

describe("columns", () => {
  it("lists only the standards actually present, sorted", () => {
    expect(matrixFor(view, standardOf, all).columns).toEqual(["ds55", "mr65", "mr86", NO_STANDARD]);
  });

  it("keeps a system conforming to nothing as its own column, last", () => {
    const m = matrixFor(view, standardOf, all);
    expect(m.columns[m.columns.length - 1]).toBe(NO_STANDARD);
    const depot = m.rows.find((r) => r.label === "Service Depot")!;
    expect(depot.cells[NO_STANDARD].count).toBe(1);
  });
});

describe("rows follow each root's own cut", () => {
  it("gives a root row and one indented row per cut node", () => {
    const m = matrixFor(view, standardOf, all);
    expect(m.rows.map((r) => [r.label, r.indent])).toEqual([
      ["Headquarters", false],
      ["East Building", true],
      ["West Building", true],
      ["Service Depot", false],
      ["Bay 1", true],
      ["Bay 2", true],
    ]);
  });

  it("does not assume a depth: depot cuts at rooms, hq at buildings", () => {
    const m = matrixFor(view, standardOf, all);
    const types = m.rows.filter((r) => r.indent).map((r) => r.type);
    expect(types).toEqual(["building", "building", "room", "room"]);
  });

  it("rolls a root's counts up over everything beneath it", () => {
    const hq = matrixFor(view, standardOf, all).rows[0];
    expect(hq.cells["mr65"].count).toBe(2);
    expect(hq.cells["mr86"].count).toBe(1);
    expect(hq.cells["mr65"].counts.outage).toBe(1);
  });

  it("leaves a cell absent rather than zero where a standard is not present", () => {
    const west = matrixFor(view, standardOf, all).rows.find((r) => r.label === "West Building")!;
    expect(west.cells["mr65"].count).toBe(2);
    expect(west.cells["mr86"]).toBeUndefined();
  });
});

describe("the filter", () => {
  it("drops rows with nothing needing attention", () => {
    const m = matrixFor(view, standardOf, attention);
    expect(m.rows.map((r) => r.label)).toEqual(["Headquarters", "East Building", "West Building"]);
  });

  it("narrows the columns to the standards that still have something", () => {
    expect(matrixFor(view, standardOf, attention).columns).toEqual(["mr65", "mr86"]);
  });
});

describe("density", () => {
  it("stays a grid at this size", () => {
    expect(matrixFor(view, standardOf, all).dense).toBe(false);
  });

  it("becomes a report past the threshold, and stops emitting sub-rows", () => {
    const many = {
      locations: view.locations,
      systems: Array.from({ length: 200 }, (_, i) => sys(`bulk-${i}`, `Bulk ${i}`, "west", "healthy")),
    } as unknown as FleetView;
    const m = matrixFor(many, () => "mr65", all);
    expect(m.dense).toBe(true);
    expect(m.rows.every((r) => !r.indent)).toBe(true);
  });
});
