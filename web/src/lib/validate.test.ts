import { describe, it, expect } from "vitest";
import { handleError, emailError } from "./validate";

// The inline rules mirror the server's Huma pattern/format (see the API validation
// test in internal/api/principals_validation_test.go): a handle is a lowercase
// username or group name, an email must look like one. Empty is not a format error
// (a separate `required` check owns emptiness), so both return null for "".
describe("handleError", () => {
  it("accepts a lowercase handle with digits and . _ -", () => {
    for (const ok of ["jordan", "field-crew", "tech-east", "a1", "x.y_z-0"]) {
      expect(handleError(ok)).toBeNull();
    }
  });
  it("rejects capitals with a no-capitals message", () => {
    expect(handleError("Jordan")).toMatch(/capital/i);
  });
  it("rejects spaces with a no-spaces message", () => {
    expect(handleError("field crew")).toMatch(/space/i);
  });
  it("rejects a leading separator and stray symbols", () => {
    expect(handleError("-lead")).not.toBeNull();
    expect(handleError("jordan@x")).not.toBeNull();
  });
  it("treats empty as not-an-error (required is separate)", () => {
    expect(handleError("")).toBeNull();
  });
});

describe("emailError", () => {
  it("accepts a well-formed address", () => {
    expect(emailError("jordan@example.com")).toBeNull();
  });
  it("rejects an obvious non-email", () => {
    expect(emailError("not-an-email")).not.toBeNull();
    expect(emailError("a@b")).not.toBeNull();
    expect(emailError("a b@c.com")).not.toBeNull();
  });
  it("treats empty as not-an-error (email is optional)", () => {
    expect(emailError("")).toBeNull();
  });
});
