import { describe, expect, it } from "vitest";
import { queryRetry } from "./queryDefaults";
import { deniedError, isDenied } from "../api/client";

describe("queryRetry", () => {
  it("never retries a refusal: a 403 is the answer, not a fault", () => {
    expect(queryRetry(0, deniedError({ status: 403 }))).toBe(false);
  });
  it("retries an ordinary failure a few times", () => {
    expect(queryRetry(0, new Error("x"))).toBe(true);
    expect(queryRetry(2, new Error("x"))).toBe(true);
    expect(queryRetry(3, new Error("x"))).toBe(false);
  });
  it("tells a refusal from other errors", () => {
    expect(isDenied(deniedError("forbidden"))).toBe(true);
    expect(isDenied({ status: 403 })).toBe(true);
    expect(isDenied({ status: 500 })).toBe(false);
    expect(isDenied(null)).toBe(false);
    expect(isDenied("nope")).toBe(false);
  });
});
