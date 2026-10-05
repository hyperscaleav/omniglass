import { describe, it, expect } from "vitest";
import { ancestors, childrenIndex, locationIndex, type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The fixture: two roots of different depth, no level in common below the
// root.
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

// hq's own recorded verdict deliberately DISAGREES with the fold over its
// in-scope clusters (healthy vs degraded): the band chip renders the recorded
// row (a band that contradicts its own detail page one click away is a
// visible bug), and the fold stays available as what it is.
const view: FleetView = {
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
} as unknown as FleetView;

describe("ancestors", () => {
  it("reads root first, which is the order the breadcrumb renders", () => {
    expect(ancestors(uuidFor("l-r1"), locationIndex(view)).map((l) => l.name)).toEqual(["hq", "hq-b1", "hq-r1"]);
  });
});

describe("childrenIndex", () => {
  it("inverts the parent pointers, so a place knows what sits beneath it", () => {
    const kids = childrenIndex(view);
    expect((kids.get(uuidFor("l-b1")) ?? []).map((l) => l.name).sort()).toEqual(["hq-r1", "hq-r2"]);
    expect(kids.get(uuidFor("l-r1"))).toBeUndefined();
  });
});
