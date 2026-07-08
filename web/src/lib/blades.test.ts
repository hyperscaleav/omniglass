import { describe, it, expect } from "vitest";
import { createRoot } from "solid-js";
import { createBladeController } from "./blades";

describe("blade controller", () => {
  it("pushes, pops, and clears", () =>
    createRoot((dispose) => {
      const c = createBladeController();
      c.push({ kind: "user", id: "a" });
      c.push({ kind: "group", id: "x" });
      expect(c.stack().map((r) => r.id)).toEqual(["a", "x"]);
      c.pop();
      expect(c.stack().map((r) => r.id)).toEqual(["a"]);
      c.close();
      expect(c.stack()).toEqual([]);
      dispose();
    }));

  it("truncates to an existing ref instead of stacking a duplicate (cycle-safe)", () =>
    createRoot((dispose) => {
      const c = createBladeController();
      c.push({ kind: "user", id: "a" });
      c.push({ kind: "group", id: "x" });
      c.push({ kind: "user", id: "a" }); // revisit root -> fold back to it
      expect(c.stack()).toEqual([{ kind: "user", id: "a" }]);
      dispose();
    }));

  it("filters the stack in place (refetch prune), no-op when nothing drops", () =>
    createRoot((dispose) => {
      const c = createBladeController();
      c.push({ kind: "user", id: "a" });
      c.push({ kind: "group", id: "x" });
      c.filter((r) => r.id !== "x"); // x deleted upstream
      expect(c.stack().map((r) => r.id)).toEqual(["a"]);
      c.filter(() => true); // nothing to drop
      expect(c.stack().map((r) => r.id)).toEqual(["a"]);
      // isTop reflects the current top
      expect(c.isTop(0)).toBe(true);
      dispose();
    }));
});
