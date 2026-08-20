import { describe, expect, it } from "vitest";
import { slotStrip } from "./slot_strip";
import type { FleetHealth } from "./health";

// The system card's slot strip (#630 design): one square per slot the
// standard wants filled, over the ACTIVE roles' quorum, with the filled ones
// carrying the occupant's state and the empty ones outlined. Plus the gap
// line under it, in the design's own words. Pure over the health read.

const h = (roles: object[]): FleetHealth => ({ verdict: "degraded", roles, systems: [], transitions: [] }) as unknown as FleetHealth;

describe("slotStrip", () => {
  it("draws quorum squares per active role, filled first with the occupant state, then empty ones", () => {
    const strip = slotStrip(h([
      { name: "mic", label: "Mic", quorum: 2, satisfying: 1, short: 1, impaired: true, active: true, impact: "degraded", assigned_to: ["m1"], down: [] },
      { name: "disp", label: "Display", quorum: 1, satisfying: 1, short: 0, impaired: false, active: true, impact: "outage", assigned_to: ["d1"], down: [] },
    ]));
    expect(strip.squares.map((s) => s.kind)).toEqual(["filled", "empty", "filled"]);
    expect(strip.filled).toBe(2);
    expect(strip.total).toBe(3);
  });

  it("marks a filled square down when its occupant is", () => {
    const strip = slotStrip(h([
      { name: "mic", label: "Mic", quorum: 2, satisfying: 1, short: 0, impaired: true, active: true, impact: "degraded", assigned_to: ["m1", "m2"], down: ["m2"] },
    ]));
    expect(strip.squares.map((s) => s.kind)).toEqual(["filled", "down"]);
  });

  it("ignores inactive roles: a losing alternate is not outstanding work", () => {
    const strip = slotStrip(h([
      { name: "bar", label: "Bar", quorum: 1, satisfying: 1, short: 0, impaired: false, active: true, impact: "outage", assigned_to: ["b1"], down: [], choice: "conf", alternate: "aio" },
      { name: "codec", label: "Codec", quorum: 1, satisfying: 0, short: 1, impaired: true, active: false, impact: "outage", assigned_to: [], down: [], choice: "conf", alternate: "cs" },
    ]));
    expect(strip.total).toBe(1);
    expect(strip.filled).toBe(1);
  });

  it("writes the gap line in the design's words: required empty, else all filled", () => {
    expect(slotStrip(h([{ name: "d", label: "D", quorum: 1, satisfying: 0, short: 1, impaired: true, active: true, impact: "outage", assigned_to: [], down: [] }])).note).toBe("1 required slot empty");
    expect(slotStrip(h([{ name: "d", label: "D", quorum: 2, satisfying: 0, short: 2, impaired: true, active: true, impact: "outage", assigned_to: [], down: [] }])).note).toBe("2 required slots empty");
    expect(slotStrip(h([{ name: "d", label: "D", quorum: 1, satisfying: 1, short: 0, impaired: false, active: true, impact: "outage", assigned_to: ["x"], down: [] }])).note).toBe("1 of 1 slots filled");
  });

  it("an occupant down but slots all assigned reads as a down slot, not an empty one", () => {
    const strip = slotStrip(h([{ name: "d", label: "D", quorum: 1, satisfying: 0, short: 0, impaired: true, active: true, impact: "outage", assigned_to: ["x"], down: ["x"] }]));
    expect(strip.note).toBe("1 slot down");
  });
});
