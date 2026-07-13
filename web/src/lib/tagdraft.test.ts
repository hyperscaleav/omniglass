import { describe, it, expect } from "vitest";
import { keyApplies, keySuggestions, exactKey, canCoin, valueValid, isEnumKey, valueOptions, valueAllowed } from "./tagdraft";
import type { Tag } from "./tags";

const tag = (name: string, applies_to: string[] = [], propagates = true, allowed_values: string[] = []): Tag => ({ id: name, name, applies_to, propagates, allowed_values });

const registry: Tag[] = [
  tag("environment"),
  tag("category", ["component"]),
  tag("rack_position", ["location"]),
  tag("asset_id", [], false),
];

describe("keyApplies", () => {
  it("a universal key applies to any kind", () => {
    expect(keyApplies(tag("environment"), "component")).toBe(true);
    expect(keyApplies(tag("environment"), "location")).toBe(true);
  });
  it("a narrowed key applies only to its listed kinds", () => {
    expect(keyApplies(tag("category", ["component"]), "component")).toBe(true);
    expect(keyApplies(tag("category", ["component"]), "system")).toBe(false);
  });
});

describe("keySuggestions", () => {
  it("offers only keys that apply to the kind and are not already bound", () => {
    const s = keySuggestions(registry, "component", ["environment"], "");
    expect(s.map((t) => t.name)).toEqual(["asset_id", "category"]); // environment bound out, rack_position wrong kind
  });
  it("filters by a case-insensitive substring query", () => {
    const s = keySuggestions(registry, "location", [], "RACK");
    expect(s.map((t) => t.name)).toEqual(["rack_position"]);
  });
  it("excludes a key already bound", () => {
    const s = keySuggestions(registry, "location", ["rack_position"], "");
    expect(s.map((t) => t.name)).not.toContain("rack_position");
  });
});

describe("exactKey", () => {
  it("finds an exact name match", () => {
    expect(exactKey(registry, "environment")?.name).toBe("environment");
  });
  it("returns undefined for a partial or unknown name", () => {
    expect(exactKey(registry, "env")).toBeUndefined();
    expect(exactKey(registry, "nope")).toBeUndefined();
  });
});

describe("canCoin", () => {
  it("is true for a new non-empty name when the caller may create keys", () => {
    expect(canCoin(registry, "new_key", true)).toBe(true);
  });
  it("is false without the create permission", () => {
    expect(canCoin(registry, "new_key", false)).toBe(false);
  });
  it("is false for an existing key or empty query", () => {
    expect(canCoin(registry, "environment", true)).toBe(false);
    expect(canCoin(registry, "  ", true)).toBe(false);
  });
});

describe("valueValid", () => {
  it("accepts non-empty, rejects blank", () => {
    expect(valueValid("prod")).toBe(true);
    expect(valueValid("   ")).toBe(false);
    expect(valueValid("")).toBe(false);
  });
});

describe("value domain", () => {
  const enumKey = tag("environment", [], true, ["prod", "staging", "dev"]);
  const freeKey = tag("note");

  it("isEnumKey distinguishes an enum key from a free key", () => {
    expect(isEnumKey(enumKey)).toBe(true);
    expect(isEnumKey(freeKey)).toBe(false);
  });

  it("valueOptions offers the enum set (declared order) for an enum key", () => {
    expect(valueOptions(enumKey, ["ignored"], "")).toEqual(["prod", "staging", "dev"]);
    expect(valueOptions(enumKey, [], "st")).toEqual(["staging"]);
  });

  it("valueOptions offers the distinct in-use values for a free key", () => {
    expect(valueOptions(freeKey, ["alpha", "beta"], "")).toEqual(["alpha", "beta"]);
    expect(valueOptions(freeKey, ["alpha", "beta"], "be")).toEqual(["beta"]);
  });

  it("valueAllowed enforces the enum, and lets any non-empty value through a free key", () => {
    expect(valueAllowed(enumKey, "prod")).toBe(true);
    expect(valueAllowed(enumKey, "qa")).toBe(false);
    expect(valueAllowed(freeKey, "whatever")).toBe(true);
    expect(valueAllowed(freeKey, "  ")).toBe(false);
  });
});
