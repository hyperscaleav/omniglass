import { describe, it, expect } from "vitest";
import { buildPredicate, toggleFacet, facetActive, matchOp, tokenToChip, type FilterKey, type Chip } from "./predicate";

type Row = { name: string; type: string; ports: number };
const rows: Row[] = [
  { name: "av-1", type: "display", ports: 2 },
  { name: "av-2", type: "camera", ports: 1 },
  { name: "lab-1", type: "display", ports: 4 },
];
const keys: FilterKey<Row>[] = [
  { key: "name", type: "string", hint: "substring", get: (r) => r.name },
  { key: "type", type: "string", hint: "exact", get: (r) => r.type },
  { key: "ports", type: "number", get: (r) => r.ports },
];

const apply = (chips: Chip[]) => rows.filter(buildPredicate(keys, chips));

describe("faceted predicate", () => {
  it("within a key, values are OR", () => {
    // type = display OR camera -> all three
    const chips: Chip[] = [{ key: "type", op: "eq", values: ["display", "camera"] }];
    expect(apply(chips).map((r) => r.name)).toEqual(["av-1", "av-2", "lab-1"]);
  });

  it("across keys, chips are AND", () => {
    const chips: Chip[] = [
      { key: "type", op: "eq", values: ["display"] },
      { key: "name", op: "contains", values: ["av"] },
    ];
    expect(apply(chips).map((r) => r.name)).toEqual(["av-1"]); // display AND name~av
  });

  it("an unknown key never excludes rows", () => {
    expect(apply([{ key: "ghost", op: "eq", values: ["x"] }])).toHaveLength(3);
  });

  it("numeric operators compare as numbers", () => {
    expect(apply([{ key: "ports", op: "gte", values: ["2"] }]).map((r) => r.name)).toEqual(["av-1", "lab-1"]);
  });
});

describe("toggleFacet (within-key OR, click-to-remove, empty-drop)", () => {
  it("adds a value as a new chip", () => {
    expect(toggleFacet([], "type", "display")).toEqual([{ key: "type", op: "eq", values: ["display"] }]);
  });
  it("adds a second value to the same key (OR), not a second chip", () => {
    const a = toggleFacet([], "type", "display");
    const b = toggleFacet(a, "type", "camera");
    expect(b).toHaveLength(1);
    expect(b[0].values).toEqual(["display", "camera"]);
  });
  it("toggling an active value removes it", () => {
    const a: Chip[] = [{ key: "type", op: "eq", values: ["display", "camera"] }];
    expect(toggleFacet(a, "type", "display")[0].values).toEqual(["camera"]);
  });
  it("removing the last value drops the chip", () => {
    const a: Chip[] = [{ key: "type", op: "eq", values: ["display"] }];
    expect(toggleFacet(a, "type", "display")).toEqual([]);
  });
  it("facetActive reflects membership", () => {
    const a: Chip[] = [{ key: "type", op: "eq", values: ["display"] }];
    expect(facetActive(a, "type", "display")).toBe(true);
    expect(facetActive(a, "type", "camera")).toBe(false);
  });
});

describe("matchOp + tokenToChip", () => {
  it("eq matches string case-insensitively and numerically", () => {
    expect(matchOp("eq", "Display", "display")).toBe(true);
    expect(matchOp("eq", 2, "2")).toBe(true);
  });
  it("contains / starts / ends", () => {
    expect(matchOp("contains", "av-1", "v-")).toBe(true);
    expect(matchOp("starts", "av-1", "av")).toBe(true);
    expect(matchOp("ends", "av-1", "-1")).toBe(true);
  });
  it("parses a bare token against the fallback key", () => {
    expect(tokenToChip("av", keys, "name")).toEqual({ key: "name", op: "contains", values: ["av"] });
  });
  it("parses key:op:value with a glyph prefix", () => {
    expect(tokenToChip("ports:>=2", keys, "name")).toEqual({ key: "ports", op: "gte", values: ["2"] });
  });
  it("rejects an empty value", () => {
    expect(tokenToChip("name:", keys, "name")).toBeNull();
  });
});

import { opsFor, valueless, tagFilterKeys, tagKeysOf } from "./predicate";

type Tagged = { name: string; tags: Record<string, string> };
const tagged: Tagged[] = [
  { name: "codec-1", tags: { environment: "prod", vendor: "cisco" } },
  { name: "codec-2", tags: { environment: "staging" } },
  { name: "display-1", tags: {} },
];

describe("presence operators (exists / absent)", () => {
  it("opsFor offers exists/absent only for a presence-capable key", () => {
    expect(opsFor("string")).not.toContain("exists");
    expect(opsFor("string", true)).toContain("exists");
    expect(opsFor("string", true)).toContain("absent");
  });

  it("matchOp exists is true for a set value, false for empty", () => {
    expect(matchOp("exists", "prod", "")).toBe(true);
    expect(matchOp("exists", "", "")).toBe(false);
    expect(matchOp("absent", "", "")).toBe(true);
    expect(matchOp("absent", "prod", "")).toBe(false);
  });

  it("valueless flags the presence operators", () => {
    expect(valueless("exists")).toBe(true);
    expect(valueless("absent")).toBe(true);
    expect(valueless("eq")).toBe(false);
  });
});

describe("tag facets", () => {
  const tagKeys = tagFilterKeys<Tagged>(tagKeysOf(tagged), new Set(["name"]));
  const withStatic = [{ key: "name", type: "string" as const, get: (r: Tagged) => r.name }, ...tagKeys];
  const apply = (chips: Chip[]) => tagged.filter(buildPredicate(withStatic, chips));

  it("derives one facet per tag key present on the rows, sorted", () => {
    expect(tagKeys.map((k) => k.key)).toEqual(["environment", "vendor"]);
  });

  it("excludes a tag key that collides with a static facet", () => {
    const keys = tagFilterKeys<Tagged>(["name", "environment"], new Set(["name"]));
    expect(keys.map((k) => k.key)).toEqual(["environment"]);
  });

  it("filters rows by a tag value (eq)", () => {
    expect(apply([{ key: "environment", op: "eq", values: ["prod"] }]).map((r) => r.name)).toEqual(["codec-1"]);
  });

  it("filters by tag presence (exists) and absence (absent)", () => {
    expect(apply([{ key: "vendor", op: "exists", values: [] }]).map((r) => r.name)).toEqual(["codec-1"]);
    expect(apply([{ key: "environment", op: "absent", values: [] }]).map((r) => r.name)).toEqual(["display-1"]);
  });

  it("autocompletes the distinct values in use for a key", () => {
    const env = tagKeys.find((k) => k.key === "environment")!;
    expect(env.values!(tagged)).toEqual(["prod", "staging"]);
  });

  it("tokenToChip parses value-less presence tokens on a presence key", () => {
    expect(tokenToChip("environment:?", withStatic, "name")).toEqual({ key: "environment", op: "exists", values: [] });
    expect(tokenToChip("vendor:!?", withStatic, "name")).toEqual({ key: "vendor", op: "absent", values: [] });
  });

  it("does not treat a presence token as value-less on a non-presence key", () => {
    // "name:?" on the non-presence name key parses "?" as a literal value, not exists.
    const c = tokenToChip("name:?", withStatic, "name");
    expect(c).toEqual({ key: "name", op: "contains", values: ["?"] });
  });
});
