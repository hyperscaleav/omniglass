import { describe, it, expect } from "vitest";
import { validateField } from "./settingsValidation";

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
});
