import { describe, it, expect } from "vitest";
import { handleError, emailError, passwordError, isPasswordPolicyMessage, MIN_PASSWORD_LENGTH } from "./validate";

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

describe("passwordError", () => {
  it("accepts a long password that does not contain the username", () => {
    expect(passwordError("orange-boat-42x", "jordan")).toBeNull();
  });
  it("rejects a password shorter than the floor", () => {
    expect(passwordError("a".repeat(MIN_PASSWORD_LENGTH - 1))).toMatch(/at least/i);
  });
  it("rejects a password that contains the username (case-insensitive)", () => {
    expect(passwordError("my-Jordan-pass-9", "jordan")).toMatch(/username/i);
  });
  it("ignores a very short username for containment", () => {
    expect(passwordError("aviation-safety-plan", "av")).toBeNull();
  });
  it("treats empty as not-an-error (a separate required check owns it)", () => {
    expect(passwordError("", "jordan")).toBeNull();
  });
});

describe("isPasswordPolicyMessage", () => {
  it("matches the server's password-policy messages", () => {
    expect(isPasswordPolicyMessage("password is too common; choose a less predictable one")).toBe(true);
    expect(isPasswordPolicyMessage("password must be at least 12 characters")).toBe(true);
    expect(isPasswordPolicyMessage("password must not contain the username")).toBe(true);
  });
  it("does not match unrelated errors", () => {
    expect(isPasswordPolicyMessage("username already exists")).toBe(false);
    expect(isPasswordPolicyMessage("current password is incorrect")).toBe(false);
    expect(isPasswordPolicyMessage(null)).toBe(false);
  });
});
