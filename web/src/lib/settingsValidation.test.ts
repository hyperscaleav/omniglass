import { describe, it, expect } from "vitest";
import { listText, textList, validateField } from "./settingsValidation";

describe("validateField", () => {
  it("rejects a value outside the enum", () => {
    expect(validateField({ type: "string", enum: ["a", "b"] }, "c")).toMatch(/one of/i);
  });
  it("accepts a value in the enum", () => {
    expect(validateField({ type: "string", enum: ["a", "b"] }, "a")).toBeNull();
  });
  it("rejects a non-numeric value for a number field", () => {
    expect(validateField({ type: "integer" }, "abc")).toMatch(/number/i);
  });
  it("rejects a value failing a pattern", () => {
    expect(validateField({ type: "string", pattern: "^[a-z]+$" }, "AB1")).toMatch(/match/i);
  });
  it("flags an unknown field", () => {
    expect(validateField(undefined, "x")).toMatch(/unknown/i);
  });
  it("accepts a well-formed list", () => {
    expect(validateField({ type: "array" }, "AV, DSP")).toBeNull();
  });
  it("accepts an empty list", () => {
    expect(validateField({ type: "array" }, "  ")).toBeNull();
  });
  it("rejects a list with a blank entry", () => {
    expect(validateField({ type: "array" }, "AV,,DSP")).toMatch(/empty entry/i);
  });
});

// The text-to-array pair is what keeps the control writable: whatever the row
// renders has to parse back into the array the API takes.
describe("a list setting round trips through its text form", () => {
  it("joins for display and splits back", () => {
    expect(textList(listText(["AV", "DSP", "QM55"]))).toEqual(["AV", "DSP", "QM55"]);
  });
  it("trims the spacing an operator types", () => {
    expect(textList("AV ,DSP,   QM55")).toEqual(["AV", "DSP", "QM55"]);
  });
  it("reads a blank line as an empty list, not a blank entry", () => {
    expect(textList("   ")).toEqual([]);
  });
});
