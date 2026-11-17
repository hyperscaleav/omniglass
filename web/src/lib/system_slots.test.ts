import { describe, it, expect } from "vitest";
import { staffingLabel, standardRolesKey, swapPath, systemRolesKey } from "./system_roles";

// The staffing line every roles surface reads a quorum through. It is pure, so it
// is tested without a server.
describe("staffingLabel", () => {
  it("reads the quorum against the fill count", () => {
    expect(staffingLabel({ quorum: 2, assigned: 1 })).toBe("2 wanted, 1 assigned");
  });

  it("says so when a role wants one and has none", () => {
    expect(staffingLabel({ quorum: 1, assigned: 0 })).toBe("1 wanted, 0 assigned");
  });

  // Over-staffing is reported, not hidden: a role with more than its quorum still
  // reads what it wanted beside what it got.
  it("reports a role filled past its quorum as it stands", () => {
    expect(staffingLabel({ quorum: 1, assigned: 3 })).toBe("1 wanted, 3 assigned");
  });
});

// One cache namespace per arc, so a standard and a system that share an address
// never collide.
describe("role cache keys", () => {
  it("keeps the system arc and the standard arc apart", () => {
    expect([...systemRolesKey("meeting-room")]).not.toEqual([...standardRolesKey("meeting-room")]);
  });
});

// swapPath is the bridge between the console's generic drag-reorder primitive
// (moveItem, listmodel.ts) and the server's only reorder route, a pairwise
// swap of REAL positions. Every "reproduces moveItem" case replays swapPath's
// swaps against a (real position -> occupant) map, then reads the result back
// out in ascending-position order (exactly how the server serves assigned_to,
// #626), so the check is end to end: did the sequence of position swaps this
// function hands to the wire produce the same on-screen order moveItem would
// have, not merely "did some bookkeeping array end up looking right".
function applyByPosition<T>(byPosition: Map<number, T>, path: [number, number][]): T[] {
  const m = new Map(byPosition);
  for (const [a, b] of path) {
    const tmp = m.get(a)!;
    m.set(a, m.get(b)!);
    m.set(b, tmp);
  }
  return [...m.keys()].sort((x, y) => x - y).map((p) => m.get(p)!);
}

describe("swapPath", () => {
  it("is empty when from equals to", () => {
    expect(swapPath([1, 2, 3], 2, 2)).toEqual([]);
  });

  it("returns real position pairs, stepping toward the target", () => {
    expect(swapPath([1, 2, 3], 0, 2)).toEqual([
      [1, 2],
      [2, 3],
    ]);
  });

  it("reproduces moveItem's reorder moving right, on a dense (gap-free) role", async () => {
    const { moveItem } = await import("./listmodel");
    const list = ["a", "b", "c", "d"];
    const positions = [1, 2, 3, 4];
    const byPosition = new Map(list.map((name, i) => [positions[i], name]));
    expect(applyByPosition(byPosition, swapPath(positions, 0, 3))).toEqual(moveItem(list, 0, 3));
  });

  it("reproduces moveItem's reorder moving left, on a dense (gap-free) role", async () => {
    const { moveItem } = await import("./listmodel");
    const list = ["a", "b", "c", "d"];
    const positions = [1, 2, 3, 4];
    const byPosition = new Map(list.map((name, i) => [positions[i], name]));
    expect(applyByPosition(byPosition, swapPath(positions, 3, 1))).toEqual(moveItem(list, 3, 1));
  });

  // The real case (#626): an unassign leaves a gap rather than compacting, so
  // a role's occupied positions are not 1..N. Task 9 review, finding C3: the
  // prior version assumed index i held position i+1, which either swapped an
  // unoccupied position (a 404 against a reorder the operator just
  // performed) or silently reordered the wrong pair.
  it("swaps real positions across a gap, not index-plus-one", () => {
    // Occupants hold positions [1, 3] (position 2 is vacant). Dragging index
    // 0 onto index 1 must swap the REAL positions 1 and 3, never 1 and 2:
    // position 2 holds nobody, and swapping into it 404s.
    expect(swapPath([1, 3], 0, 1)).toEqual([[1, 3]]);
  });

  it("never mentions a vacant position across a multi-step gapped drag", () => {
    // Occupants hold positions [1, 2, 4] (position 3 is vacant).
    const path = swapPath([1, 2, 4], 0, 2);
    for (const [a, b] of path) {
      expect(a).not.toBe(3);
      expect(b).not.toBe(3);
    }
  });

  it("reproduces moveItem's reorder even when positions are gapped", async () => {
    const { moveItem } = await import("./listmodel");
    // Occupants at real positions [1, 2, 4] (position 3 vacant).
    const list = ["one", "two", "four"];
    const positions = [1, 2, 4];
    const byPosition = new Map(list.map((name, i) => [positions[i], name]));
    expect(applyByPosition(byPosition, swapPath(positions, 0, 2))).toEqual(moveItem(list, 0, 2));
  });
});
