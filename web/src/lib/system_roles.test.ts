import { describe, it, expect } from "vitest";
import { roleByComponent, staffingLabel, standardRolesKey, systemRolesKey, type EffectiveRole } from "./system_roles";

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
