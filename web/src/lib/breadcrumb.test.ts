import { describe, expect, it } from "vitest";
import { collapseTrail, crumbLayout, trailBudget, TRAIL_BUDGET, type Crumb } from "./breadcrumb";

// The ellipsis rule as arithmetic: the last two crumbs never shrink, every
// earlier one may ellipsise down to a 26px stub. Pure, so the rule is pinned
// here once and the component only applies styles.
describe("crumbLayout", () => {
  it("a single crumb never shrinks", () => {
    expect(crumbLayout(1)).toEqual([{ flex: "none", minWidth: "auto" }]);
  });

  it("two crumbs never shrink", () => {
    expect(crumbLayout(2)).toEqual([
      { flex: "none", minWidth: "auto" },
      { flex: "none", minWidth: "auto" },
    ]);
  });

  it("three crumbs: the first may shrink to a stub, the last two hold", () => {
    expect(crumbLayout(3)).toEqual([
      { flex: "0 1 auto", minWidth: "26px" },
      { flex: "none", minWidth: "auto" },
      { flex: "none", minWidth: "auto" },
    ]);
  });

  it("five crumbs: every crumb before the last two shrinks", () => {
    const styles = crumbLayout(5);
    expect(styles).toHaveLength(5);
    for (const s of styles.slice(0, 3)) expect(s).toEqual({ flex: "0 1 auto", minWidth: "26px" });
    for (const s of styles.slice(3)) expect(s).toEqual({ flex: "none", minWidth: "auto" });
  });

  it("zero crumbs is an empty layout, not a crash", () => {
    expect(crumbLayout(0)).toEqual([]);
  });
});

describe("trailBudget", () => {
  it("scales with the measured width, never below a first-and-last floor", () => {
    expect(trailBudget(900)).toBeGreaterThan(trailBudget(300));
    expect(trailBudget(0)).toBe(TRAIL_BUDGET);
    expect(trailBudget(40)).toBeGreaterThanOrEqual(12);
  });
});

describe("collapseTrail", () => {
  const c = (label: string): Crumb => ({ key: label, label });
  it("keeps a trail that fits whole", () => {
    expect(collapseTrail([c("HQ"), c("Floor 2")]).hidden).toEqual([]);
  });
  it("folds the middle of a long trail, keeping the first and the last", () => {
    const r = collapseTrail([c("Explore"), c("Headquarters campus"), c("Floor 2 west wing"), c("Room 201 huddle"), c("Rack")]);
    expect(r.head.map((x) => x.label)).toEqual(["Explore"]);
    expect(r.hidden.map((x) => x.label)).toEqual(["Headquarters campus", "Floor 2 west wing", "Room 201 huddle"]);
    expect(r.tail.map((x) => x.label)).toEqual(["Rack"]);
  });
  it("folds a trail a narrow measured budget cannot hold", () => {
    const t = [c("HQ"), c("Floor 2"), c("Room 201")];
    expect(collapseTrail(t, trailBudget(1000)).hidden).toEqual([]);
    expect(collapseTrail(t, trailBudget(100)).hidden.map((x) => x.label)).toEqual(["Floor 2"]);
  });
});
