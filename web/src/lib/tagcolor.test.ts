import { describe, it, expect } from "vitest";
import { tagHue, TAG_HUES } from "./tagcolor";

describe("tagHue", () => {
  it("is deterministic: the same key always maps to the same hue", () => {
    expect(tagHue("environment")).toBe(tagHue("environment"));
    expect(tagHue("cost_center")).toBe(tagHue("cost_center"));
  });

  it("only ever returns a hue from the curated ramp", () => {
    for (const k of ["environment", "category", "cost_center", "compliance", "asset_id", "tier", "vendor", "room", "x", ""]) {
      expect(TAG_HUES).toContain(tagHue(k));
    }
  });

  it("spreads distinct keys across the ramp (not all one color)", () => {
    const keys = ["environment", "category", "cost_center", "compliance", "asset_id", "tier", "vendor", "room", "owner", "maintenance_window"];
    const hues = new Set(keys.map(tagHue));
    // Not a strict guarantee, but a hash into a 12-entry ramp over 10 varied keys
    // should land on several distinct hues; one bucket would signal a broken hash.
    expect(hues.size).toBeGreaterThan(3);
  });

  it("does not depend on insertion order or surrounding state", () => {
    const a = tagHue("environment");
    tagHue("something-else");
    expect(tagHue("environment")).toBe(a);
  });
});
