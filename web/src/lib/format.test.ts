import { describe, it, expect } from "vitest";
import { describeError } from "./format";

// describeError is the one place a thrown API error becomes a sentence an
// operator reads, so what it drops is what no surface can show.
describe("describeError", () => {
  it("reads the problem body's detail", () => {
    expect(describeError({ title: "Conflict", detail: "the name is taken", status: 409 })).toBe("the name is taken");
  });

  it("falls back to the title, then to a generic line", () => {
    expect(describeError({ title: "Conflict", status: 409 })).toBe("Conflict");
    expect(describeError(null)).toBe("The operation failed.");
    expect(describeError(new Error("boom"))).toBe("The operation failed.");
  });

  // A schema refusal is the case the head line cannot carry: Huma's detail is
  // always the literal "validation failed" and the actionable half is in
  // errors[]. A surface that showed only the head told an operator nothing about
  // which field to fix.
  it("names the field and the reason on a schema refusal", () => {
    expect(describeError({
      title: "Unprocessable Entity",
      detail: "validation failed",
      status: 422,
      errors: [{ message: "expected length <= 90", location: "body.name_rule.stem" }],
    })).toBe("validation failed: name_rule.stem: expected length <= 90");
  });

  // One value failing two keywords reports twice and says one thing, so the
  // duplicates collapse rather than reading as two problems.
  it("collapses duplicate field errors and keeps a location-less one", () => {
    expect(describeError({
      detail: "validation failed",
      errors: [
        { message: "expected length >= 1", location: "body.stem" },
        { message: "expected length >= 1", location: "body.stem" },
        { message: "unknown field" },
        { message: "   " },
      ],
    })).toBe("validation failed: stem: expected length >= 1; unknown field");
  });
});
