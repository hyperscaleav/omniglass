import { describe, it, expect } from "vitest";
import { INBOX_COUNT_KEY, INBOX_PREFIX, badgeText, inboxEntryHref, inboxListKey } from "./inbox";

describe("badgeText", () => {
  it("says nothing at zero or before the count lands", () => {
    expect(badgeText(0)).toBe("");
    expect(badgeText(undefined)).toBe("");
  });
  it("says the count up to 99, and 99+ beyond", () => {
    expect(badgeText(1)).toBe("1");
    expect(badgeText(99)).toBe("99");
    expect(badgeText(100)).toBe("99+");
  });
});

describe("inbox keys", () => {
  // One invalidation of the prefix must reach the bell and both list filters.
  it("hangs the count and both list filters off one prefix", () => {
    for (const k of [INBOX_COUNT_KEY, inboxListKey(true), inboxListKey(false)]) {
      expect(k.slice(0, INBOX_PREFIX.length)).toEqual([...INBOX_PREFIX]);
    }
    expect(inboxListKey(true)).not.toEqual(inboxListKey(false));
  });
});

describe("inboxEntryHref", () => {
  it("leads to the component's Activity, by uuid", () => {
    expect(inboxEntryHref({ component_id: "c-1" })).toBe("/components/c-1?tab=activity");
  });
});
