import { describe, expect, it } from "vitest";
import { foldToBudget, frameLayout, labelsAffordable, roomBoxesAffordable, type LabelMode } from "./view_budgets";

// The three budgets (#838). Each answers "can this view afford it", so an
// operator does not have to hold the answer in their head. They are pure
// because every one of them is arithmetic about the space available, and the
// renderer that consumes them should have nothing left to decide.

describe("the label budget", () => {
  it("affords labels while the rooms in view stay under the ceiling", () => {
    expect(labelsAffordable(3, "auto")).toBe(true);
    expect(labelsAffordable(24, "auto")).toBe(true);
  });

  it("stops affording them past it: 602 room labels do not fit at any type size", () => {
    expect(labelsAffordable(25, "auto")).toBe(false);
    expect(labelsAffordable(602, "auto")).toBe(false);
  });

  // The fleet level has the whole estate's rooms in view and a drilled card has
  // a handful, so one ceiling produces both behaviours with no second rule.
  it("turns names off across a whole estate and back on inside one card", () => {
    expect(labelsAffordable(42, "auto")).toBe(false);
    expect(labelsAffordable(3, "auto")).toBe(true);
  });

  it("lets always and off override the arithmetic in both directions", () => {
    expect(labelsAffordable(602, "always")).toBe(true);
    expect(labelsAffordable(1, "off")).toBe(false);
  });

  it("drops room boxes before labels, since a box costs more width than a name", () => {
    // At the same count, boxes need the operator to have asked for them AND
    // the budget to allow them; labels only need the budget.
    expect(labelsAffordable(20, "auto")).toBe(true);
    expect(roomBoxesAffordable(20, "auto", true)).toBe(true);
    expect(roomBoxesAffordable(20, "auto", false)).toBe(false);
    expect(roomBoxesAffordable(602, "auto", true)).toBe(false);
  });

  it("honours the operator's box toggle even when labels are forced on", () => {
    expect(roomBoxesAffordable(602, "always", true)).toBe(true);
    expect(roomBoxesAffordable(602, "always", false)).toBe(false);
    expect(roomBoxesAffordable(1, "off", true)).toBe(false);
  });

  it("treats a ceiling of zero as never affordable, not as always", () => {
    expect(labelsAffordable(0, "auto", 0)).toBe(false);
  });
});

describe("the area budget", () => {
  const weight = (n: number) => n;

  it("draws everything when everything clears the floor", () => {
    const { drawn, folded } = foldToBudget([10, 10, 10, 10], weight, 40_000);
    expect(drawn).toHaveLength(4);
    expect(folded).toHaveLength(0);
  });

  it("folds what would draw smaller than a clickable tile", () => {
    // One tile takes almost all the weight, so the rest would be slivers.
    const { drawn, folded } = foldToBudget([1000, 1, 1, 1, 1], weight, 40_000);
    expect(drawn).toEqual([1000]);
    expect(folded).toEqual([1, 1, 1, 1]);
  });

  it("caps the tile count however generous the pool", () => {
    const many = Array.from({ length: 200 }, () => 1);
    const { drawn, folded } = foldToBudget(many, weight, 4_000_000, { maxTiles: 48 });
    expect(drawn).toHaveLength(48);
    expect(folded).toHaveLength(152);
  });

  it("orders by weight so the folded remainder is always the smallest", () => {
    const { drawn, folded } = foldToBudget([1, 500, 2, 400], weight, 40_000);
    expect(drawn[0]).toBe(500);
    expect(Math.max(...folded)).toBeLessThan(Math.min(...drawn));
  });

  it("always draws at least one tile rather than folding everything away", () => {
    const { drawn, folded } = foldToBudget([1, 1, 1], weight, 10);
    expect(drawn).toHaveLength(1);
    expect(folded).toHaveLength(2);
  });

  it("folds nothing when there is nothing to fold", () => {
    expect(foldToBudget([], weight, 40_000)).toEqual({ drawn: [], folded: [] });
  });

  it("ignores a zero or negative weight rather than dividing by it", () => {
    const { drawn, folded } = foldToBudget([0, 0], weight, 40_000);
    expect(drawn.length + folded.length).toBe(2);
  });
});

describe("the z-order budget", () => {
  it("reserves the header as space, so contents cannot be drawn over it", () => {
    const box = frameLayout(300, 200);
    expect(box.headerH).toBeGreaterThan(0);
    expect(box.y).toBeGreaterThanOrEqual(box.headerH);
    expect(box.y + box.h).toBeLessThanOrEqual(200);
    expect(box.x + box.w).toBeLessThanOrEqual(300);
  });

  it("gives up the header rather than the content when the frame is small", () => {
    const box = frameLayout(60, 24);
    expect(box.headerH).toBe(0);
    expect(box.w).toBeGreaterThan(0);
    expect(box.h).toBeGreaterThan(0);
  });

  it("never returns a negative or fractional box", () => {
    for (const [w, h] of [
      [0, 0],
      [1, 1],
      [7, 300],
      [1000, 3],
    ]) {
      const box = frameLayout(w, h);
      expect(box.w).toBeGreaterThanOrEqual(0);
      expect(box.h).toBeGreaterThanOrEqual(0);
      expect(Number.isInteger(box.x)).toBe(true);
      expect(Number.isInteger(box.y)).toBe(true);
      expect(Number.isInteger(box.w)).toBe(true);
      expect(Number.isInteger(box.h)).toBe(true);
    }
  });
});

describe("the budgets are arithmetic, not preference", () => {
  it("gives the same answer for the same inputs, whatever the mode's spelling", () => {
    const modes: LabelMode[] = ["auto", "always", "off"];
    for (const m of modes) {
      expect(labelsAffordable(30, m)).toBe(labelsAffordable(30, m));
    }
  });
});
