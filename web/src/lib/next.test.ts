import { describe, expect, it } from "vitest";
import { encodeNext, resolveNext } from "./next";

const ORIGIN = "https://omniglass.example";

describe("encodeNext", () => {
  it("carries the query string, which is the whole point of the deep link", () => {
    expect(encodeNext("/web/components", "?system=abc-123")).toBe(
      encodeURIComponent("/web/components?system=abc-123"),
    );
  });

  it("returns null for the root, so login is not decorated with a useless next", () => {
    expect(encodeNext("/web")).toBeNull();
    expect(encodeNext("/web/")).toBeNull();
    expect(encodeNext("/")).toBeNull();
  });

  it("carries a plain path with no query", () => {
    expect(encodeNext("/web/systems")).toBe(encodeURIComponent("/web/systems"));
  });
});

describe("resolveNext", () => {
  // The regression #646 is filed for: an operator on a filtered list hits a
  // session expiry, signs back in, and must land back on the SAME filtered
  // list. Dropping the query returns them to the unscoped list with no sign
  // anything was lost.
  it("round-trips a query string through encode and resolve", () => {
    const raw = encodeNext("/web/components", "?system=abc-123");
    expect(resolveNext(raw, ORIGIN)).toBe("/components?system=abc-123");
  });

  it("carries a fragment too", () => {
    const raw = encodeNext("/web/systems", "?q=av", "#roles");
    expect(resolveNext(raw, ORIGIN)).toBe("/systems?q=av#roles");
  });

  it("strips the console base so the router does not double-count it", () => {
    expect(resolveNext(encodeURIComponent("/web/locations"), ORIGIN)).toBe("/locations");
  });

  // The open-redirect guard. An attacker-supplied absolute URL must never be
  // navigated to, query string or not.
  it("refuses a cross-origin target", () => {
    expect(resolveNext(encodeURIComponent("https://evil.example/steal?x=1"), ORIGIN)).toBe("/");
  });

  it("resolves an absent, empty, or non-string value to the root", () => {
    expect(resolveNext(undefined, ORIGIN)).toBe("/");
    expect(resolveNext("", ORIGIN)).toBe("/");
    expect(resolveNext(42, ORIGIN)).toBe("/");
  });

  // decodeURIComponent throws on a lone percent; a failed redirect still has to
  // land somewhere rather than take the login screen down with it.
  it("resolves a malformed encoding to the root instead of throwing", () => {
    expect(resolveNext("%", ORIGIN)).toBe("/");
  });
});
