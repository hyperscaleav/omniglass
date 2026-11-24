import { describe, it, expect } from "vitest";
import { scopeWords, permissionLabel } from "./access";

// How the profile's Access card reads a grant's scope and a permission (#2447).
describe("scopeWords", () => {
  it("reads the all scope as everywhere", () => {
    expect(scopeWords({ role: "owner", scope_kind: "all" })).toBe("Everywhere");
  });
  it("names a bounded scope by its kind, and its subtree reach when it is not the default", () => {
    expect(scopeWords({ role: "viewer", scope_kind: "location", scope_id: "u1" })).toBe("One location");
    expect(scopeWords({ role: "viewer", scope_kind: "system", scope_id: "u1" })).toBe("One system");
    expect(scopeWords({ role: "viewer", scope_kind: "component", scope_id: "u1" })).toBe("One component");
  });
  it("passes an unknown kind through", () => {
    expect(scopeWords({ role: "x", scope_kind: "zone" })).toBe("zone");
  });
});

describe("permissionLabel", () => {
  it("spells the bare wildcard as every permission", () => {
    expect(permissionLabel(">")).toBe("Every permission");
  });
  it("leaves a real permission as it is", () => {
    expect(permissionLabel("alarm:read")).toBe("alarm:read");
  });
});
