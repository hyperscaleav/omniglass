import { describe, it, expect } from "vitest";
import { can, canFleet, type Me } from "./auth";

const me = (permissions: string[], fleet_permissions: string[]): Me => ({
  principal: { id: "p", kind: "human" },
  permissions,
  fleet_permissions,
  grants: [],
});

// canFleet is can() over the permissions an all-scope grant carries: what a
// caller may do to fleet-wide config (event rules), which a grant bounded to a
// place reaches in the flattened set but not on the server.
describe("canFleet", () => {
  it("judges the all-scope permissions, not the flattened set", () => {
    const placeOwner = me([">"], ["*:read"]);
    expect(can(placeOwner, "rule", "create")).toBe(true);
    expect(canFleet(placeOwner, "rule", "create")).toBe(false);
    expect(canFleet(placeOwner, "rule", "read")).toBe(true);
  });

  it("matches like can, read floor included", () => {
    expect(canFleet(me([], [">"]), "rule", "delete")).toBe(true);
    expect(canFleet(me([], ["rule:update"]), "rule", "update")).toBe(true);
    expect(canFleet(me([], ["rule:update"]), "rule", "read")).toBe(true);
    expect(canFleet(me([], ["rule:update"]), "rule", "delete")).toBe(false);
  });

  it("grants nothing without the field or a principal", () => {
    expect(canFleet({ ...me([">"], []), fleet_permissions: undefined }, "rule", "read")).toBe(false);
    expect(canFleet(null, "rule", "read")).toBe(false);
  });
});
