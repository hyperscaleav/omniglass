import { describe, it, expect } from "vitest";
import { roleByComponent, staffingLabel, standardRolesKey, swapPath, systemRolesKey, type EffectiveRole } from "./system_roles";

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

// roleByComponent pivots the by-role read into the by-device read: which role
// (if any) each component fills, the shape MembersPanel needs to annotate its
// member list. Pure, so it is tested without a server.
describe("roleByComponent", () => {
  const roles: EffectiveRole[] = [
    { name: "table-mic", display_name: "Table microphone", quorum: 2, impact: "degraded", accepted_types: [], pinned_products: [], position_labels: [], from_standard: true, assigned_to: ["mic-1", "mic-2"], assigned: 2, understaffed: 0 },
    { name: "main-display", display_name: "Main display", quorum: 1, impact: "outage", accepted_types: [], pinned_products: [], position_labels: [], from_standard: true, assigned_to: ["disp-1"], assigned: 1, understaffed: 0 },
  ];

  it("maps each occupant to the role it fills", () => {
    const m = roleByComponent(roles);
    expect(m.get("mic-1")?.name).toBe("table-mic");
    expect(m.get("mic-2")?.name).toBe("table-mic");
    expect(m.get("disp-1")?.name).toBe("main-display");
  });

  it("leaves a component that fills no role absent from the map, not erroring", () => {
    const m = roleByComponent(roles);
    expect(m.has("power-conditioner-1")).toBe(false);
    expect(m.get("power-conditioner-1")).toBeUndefined();
  });

  it("returns an empty map for a system with no roles", () => {
    expect(roleByComponent([]).size).toBe(0);
  });
});

// swapPath is the bridge between the console's generic drag-reorder primitive
// (moveItem, listmodel.ts) and the server's only reorder route, a pairwise
// swap. Every case here checks that replaying swapPath's swaps by hand
// reproduces exactly what moveItem would have produced, so the drag gesture
// and the server end up agreeing on the final order.
function applySwaps<T>(list: T[], path: [number, number][]): T[] {
  const a = [...list];
  for (const [p, w] of path) {
    const i = p - 1;
    const j = w - 1;
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

describe("swapPath", () => {
  it("is empty when from equals to", () => {
    expect(swapPath(2, 2)).toEqual([]);
  });

  it("returns 1-based adjacent position pairs, stepping toward the target", () => {
    expect(swapPath(0, 2)).toEqual([
      [1, 2],
      [2, 3],
    ]);
  });

  it("reproduces moveItem's reorder moving right", async () => {
    const { moveItem } = await import("./listmodel");
    const list = ["a", "b", "c", "d"];
    expect(applySwaps(list, swapPath(0, 3))).toEqual(moveItem(list, 0, 3));
  });

  it("reproduces moveItem's reorder moving left", async () => {
    const { moveItem } = await import("./listmodel");
    const list = ["a", "b", "c", "d"];
    expect(applySwaps(list, swapPath(3, 1))).toEqual(moveItem(list, 3, 1));
  });
});
