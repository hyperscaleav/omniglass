import { describe, expect, it } from "vitest";
import { planMembershipChanges, planSystemMove, type Member } from "./members";

const m = (system_id: string, primary: boolean): Member => ({ component: "mic", component_id: "c", component_health: "healthy", primary, system: system_id, system_id, system_count: 1 });

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

describe("planMembershipChanges, removing the primary's chip", () => {
  it("removes the primary on its own", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], undefined, new Set(["a"]))).toEqual([{ op: "remove", system: "a" }]);
  });

  it("removes the old primary once after promoting another", () => {
    expect(planMembershipChanges([m("a", true), m("g", false)], "g", new Set(["a"]))).toEqual([
      { op: "setPrimary", system: "g" },
      { op: "remove", system: "a" },
    ]);
  });

  it("does not remove the primary twice when it was also cleared", () => {
    expect(planMembershipChanges([m("a", true)], "", new Set(["a"]))).toEqual([{ op: "remove", system: "a" }]);
  });
});

describe("planMembershipChanges, also serves", () => {
  it("adds a system without touching the primary", () => {
    expect(planMembershipChanges([m("a", true)], undefined, new Set(), new Set(["b"]))).toEqual([{ op: "add", system: "b" }]);
  });

  it("does not add a system the component is already in", () => {
    expect(planMembershipChanges([m("a", true)], undefined, new Set(), new Set(["a"]))).toEqual([]);
  });

  it("adds an extra system once, ahead of promoting it", () => {
    expect(planMembershipChanges([m("a", true)], "b", new Set(), new Set(["b"]))).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
    ]);
  });
});

describe("planSystemMove (a component to another system)", () => {
  it("joins the destination, makes it primary, and leaves the old system", () => {
    expect(planSystemMove({ members: [m("a", true)], to: "b", fromSlots: [] })).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
      { op: "remove", system: "a" },
    ]);
  });

  it("staffs the chosen slot instead of a bare add, and frees the slot it held", () => {
    expect(planSystemMove({ members: [m("a", true)], to: "b", toSlot: "mics", fromSlots: ["dsp"] })).toEqual([
      { op: "assign", system: "b", slot: "mics" },
      { op: "setPrimary", system: "b" },
      { op: "unassign", system: "a", slot: "dsp" },
      { op: "remove", system: "a" },
    ]);
  });

  it("keeps the memberships it is not leaving", () => {
    expect(planSystemMove({ members: [m("a", true), m("g", false)], to: "b", fromSlots: [] })).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
      { op: "remove", system: "a" },
    ]);
  });

  it("does not re-add a destination it already serves, and never removes it", () => {
    expect(planSystemMove({ members: [m("a", true), m("b", false)], to: "b", fromSlots: [] })).toEqual([
      { op: "setPrimary", system: "b" },
      { op: "remove", system: "a" },
    ]);
  });

  it("has nothing to leave when the component is in no system", () => {
    expect(planSystemMove({ members: [], to: "b", fromSlots: [] })).toEqual([
      { op: "add", system: "b" },
      { op: "setPrimary", system: "b" },
    ]);
  });
});
