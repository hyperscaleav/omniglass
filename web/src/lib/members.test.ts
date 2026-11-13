import { describe, expect, it } from "vitest";
import { planMembershipChanges, type Member } from "./members";

const m = (system_id: string, primary: boolean): Member => ({ component: "mic", component_id: "c", component_verdict: "healthy", primary, system: system_id, system_id, system_count: 1 });

describe("planMembershipChanges", () => {
  it("is empty when nothing was touched", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], undefined, new Set())).toEqual([]);
    expect(planMembershipChanges([m("a", true)], "a", new Set())).toEqual([]);
  });

  it("adds then promotes a system the component is not in", () => {
    expect(planMembershipChanges([m("a", true)], "b", new Set())).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
    ]);
  });

  it("only promotes a system it is already in", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], "g", new Set())).toEqual([{ op: "setPrimary", system: "g" }]);
  });

  it("adds and promotes when the component has no membership at all", () => {
    expect(planMembershipChanges([], "b", new Set())).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
    ]);
  });

  it("removes the primary when it is cleared", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], "", new Set())).toEqual([{ op: "remove", system: "a" }]);
  });

  it("removes staged chips after the primary moves", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], "b", new Set(["g"]))).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
      { op: "remove", system: "g" },
    ]);
  });

  it("never removes the system it is promoting, even if its chip was staged for removal", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], "g", new Set(["g"]))).toEqual([{ op: "setPrimary", system: "g" }]);
  });
});
