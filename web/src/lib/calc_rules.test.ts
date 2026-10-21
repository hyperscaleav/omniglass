import { describe, it, expect } from "vitest";
import {
  type CalcRule,
  createCalcBody,
  draftFromCalcRule,
  emptyCalcDraft,
  hasErrors,
  liveWords,
  previewCalcBody,
  reduceWords,
  reducersFor,
  updateCalcBody,
  validateCalcDraft,
  valueTypeOf,
} from "./calc_rules";

const rule: CalcRule = {
  id: "11111111-1111-7111-8111-111111111111", name: "room-mean", owner_kind: "system",
  selector: { system_types: ["meeting"] }, inputs: [{ as: "t", key: "room-temperature" }], reduce: { reducer: "average" },
  output_key: "avg-temp", output_lane: "metric", enabled: true, official: false, version: 2,
  created_at: "2026-10-21T00:00:00Z", updated_at: "2026-10-21T00:00:00Z",
  owners_computed: 3, owners_pending: 1, owners_unsupported: 0, owners_failed: 0,
};
const types: Record<string, "number" | "text" | "bool"> = { "room-temperature": "number", "video-input": "text", "in-call": "bool" };
const typeOf = (k: string) => types[k];

describe("calc rules", () => {
  it("offers only the reducers that fold an input's type", () => {
    expect(reducersFor("number")).toEqual(["average", "min", "max", "sum", "count"]);
    expect(reducersFor("text")).toEqual(["count", "majority"]);
    expect(reducersFor("bool")).toEqual(["count", "majority", "any", "all"]);
    expect(valueTypeOf("metric")).toBe("number");
    expect(valueTypeOf("property", "bool")).toBe("bool");
    expect(valueTypeOf("property", "string")).toBe("text");
  });

  it("reads a reduce and the live counts in words", () => {
    expect(reduceWords(rule)).toBe("the average of room-temperature");
    expect(reduceWords({ ...rule, reduce: { expr: "a + b" } })).toBe("a + b");
    expect(liveWords(rule)).toBe("3 computed, 1 pending");
    expect(liveWords({ ...rule, owners_computed: 0, owners_pending: 0 })).toBe("nothing computed yet");
  });

  it("judges a draft the way the server will", () => {
    const d = emptyCalcDraft();
    const e = validateCalcDraft(d, typeOf);
    expect(e.name).toBeTruthy();
    expect(e.input?.[0]).toBeTruthy();
    expect(e.reduce).toBeTruthy();
    expect(e.outputKey).toBeTruthy();
    const ok = { ...d, name: "room-mean", inputs: [{ as: "t", key: "room-temperature" }], reducer: "average" as const, outputKey: "avg-temp" };
    expect(hasErrors(validateCalcDraft(ok, typeOf))).toBe(false);
    expect(validateCalcDraft({ ...ok, reducer: "any" }, typeOf).reduce).toMatch(/does not fold number/);
    expect(validateCalcDraft({ ...ok, inputs: [...ok.inputs, { as: "u", key: "in-call" }] }, typeOf).reduce).toMatch(/one input/);
    expect(validateCalcDraft({ ...ok, inputs: [{ as: "in-call", key: "in-call" }] }, typeOf).input?.[0]).toMatch(/letter or underscore/);
    expect(validateCalcDraft({ ...ok, inputs: [{ as: "a", key: "in-call" }, { as: "a", key: "video-input" }], mode: "expr", expr: "a" }, typeOf).input?.[1]).toMatch(/names two/);
    expect(validateCalcDraft({ ...ok, mode: "expr", expr: " " }, typeOf).reduce).toBeTruthy();
    // An edit keeps a stored name it would not accept anew.
    expect(validateCalcDraft({ ...ok, name: "Old_Name" }, typeOf, "Old_Name").name).toBeUndefined();
  });

  it("sends a create body, an edit's changes only, and a preview candidate", () => {
    const d = draftFromCalcRule(rule);
    expect(createCalcBody({ ...d, name: "copy" })).toEqual({
      name: "copy", owner_kind: "system", selector: { system_types: ["meeting"], systems: [] },
      inputs: [{ as: "t", key: "room-temperature" }], reduce: { reducer: "average" }, output_key: "avg-temp", enabled: true,
    });
    expect(updateCalcBody(d, rule)).toEqual({});
    expect(updateCalcBody({ ...d, mode: "expr", expr: "len(members) * 1.0" }, rule)).toEqual({ reduce: { expr: "len(members) * 1.0" } });
    expect(updateCalcBody({ ...d, enabled: false, systems: ["sys-1"] }, rule)).toEqual({
      enabled: false, selector: { system_types: ["meeting"], systems: ["sys-1"] },
    });
    expect(previewCalcBody(d, "room-mean")).toMatchObject({ name: "room-mean", owner_kind: "system", output_key: "avg-temp" });
    expect(previewCalcBody(d).name).toBeUndefined();
  });
});
