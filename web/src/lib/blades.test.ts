import { describe, it, expect } from "vitest";
import { createRoot } from "solid-js";
import { createBladeController, createEditSlot } from "./blades";

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

describe("edit slot", () => {
  it("is not editable until the body binds, then begins/saves/exits", async () => {
    let saved = 0;
    let disposeFn = () => {};
    const slot = createRoot((dispose) => {
      disposeFn = dispose;
      return createEditSlot();
    });
    // No bind yet: not editable, no pencil.
    expect(slot.editable()).toBe(false);
    slot.bind({ save: async () => { saved++; } });
    expect(slot.editable()).toBe(true); // bound with no predicate -> editable
    expect(slot.editing()).toBe(false);
    slot.begin();
    expect(slot.editing()).toBe(true);
    await slot.save();
    expect(saved).toBe(1);
    expect(slot.editing()).toBe(false);
    disposeFn();
  });

  it("respects the bound editable predicate (permission gate)", () =>
    createRoot((dispose) => {
      const slot = createEditSlot();
      slot.bind({ editable: () => false, save: async () => {} });
      expect(slot.editable()).toBe(false);
      dispose();
    }));

  it("cancel runs the bound cancel and exits edit without saving", () =>
    createRoot((dispose) => {
      const slot = createEditSlot();
      let cancelled = 0;
      let saved = 0;
      slot.bind({ save: async () => { saved++; }, cancel: () => { cancelled++; } });
      slot.begin();
      expect(slot.editing()).toBe(true);
      slot.cancel();
      expect(cancelled).toBe(1);
      expect(saved).toBe(0);
      expect(slot.editing()).toBe(false);
      dispose();
    }));
});
