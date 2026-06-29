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
