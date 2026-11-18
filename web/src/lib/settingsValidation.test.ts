import { describe, it, expect } from "vitest";
import { listText, objectText, textList, validateField, wireValue } from "./settingsValidation";

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
  it("rejects a blank or a fraction for an integer field, which the API would refuse", () => {
    expect(validateField({ type: "integer", minimum: 1 }, "")).toMatch(/whole number/i);
    expect(validateField({ type: "integer", minimum: 1 }, "7.5")).toMatch(/whole number/i);
    expect(validateField({ type: "integer", minimum: 1 }, "7")).toBeNull();
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

describe("wireValue", () => {
  it("writes an integer field as a number", () => {
    expect(wireValue({ type: "integer" }, "7")).toBe(7);
  });
  it("writes a number field as a number", () => {
    expect(wireValue({ type: "number" }, "2.5")).toBe(2.5);
  });
  it("leaves an unparseable number as typed, for the API to refuse by name", () => {
    expect(wireValue({ type: "integer" }, "seven")).toBe("seven");
  });
  it("writes a list field as an array", () => {
    expect(wireValue({ type: "array" }, "AV, DSP")).toEqual(["AV", "DSP"]);
  });
  it("writes a string field as typed", () => {
    expect(wireValue({ type: "string" }, "omniglass-dark")).toBe("omniglass-dark");
  });
});

describe("a map setting round trips through its JSON text", () => {
  const map = { type: "object" };

  it("reads a stored map as JSON and writes the text back as the object", () => {
    const stored = { operator: { write: "never" } };
    expect(objectText(stored)).toBe('{"operator":{"write":"never"}}');
    expect(wireValue(map, objectText(stored))).toEqual(stored);
  });

  it("writes a blank map as an empty object", () => {
    expect(wireValue(map, "  ")).toEqual({});
    expect(validateField(map, "")).toBeNull();
  });

  // The API merges a map into what is stored, so a key the operator deleted
  // from the text has to go back as null or it would stay set.
  it("sends null for an entry removed from the stored map", () => {
    const stored = { operator: { write: "never" }, viewer: { read: "ask" } };
    expect(wireValue(map, '{"viewer":{"read":"ask"}}', stored)).toEqual({ operator: null, viewer: { read: "ask" } });
  });

  it("sends null for a class removed from inside a role's entry", () => {
    const stored = { operator: { write: "never", destructive: "never" } };
    expect(wireValue(map, '{"operator":{"write":"never"}}', stored)).toEqual({
      operator: { write: "never", destructive: null },
    });
  });

  it("clears every stored entry when the text is blanked", () => {
    const stored = { "fleet.devices:delete": "never", "fleet.devices:list": "ask" };
    expect(wireValue(map, "", stored)).toEqual({ "fleet.devices:delete": null, "fleet.devices:list": null });
  });

  it("refuses text that is not a JSON object", () => {
    expect(validateField(map, "{nope")).toBe("must be a JSON object");
    expect(validateField(map, "[1]")).toBe("must be a JSON object");
    expect(validateField(map, '"x"')).toBe("must be a JSON object");
    expect(validateField(map, '{"create-component":"never"}')).toBeNull();
  });
});
