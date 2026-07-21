import { describe, it, expect } from "vitest";
import { staffingLabel, standardRolesKey, systemRolesKey } from "./system_roles";

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
