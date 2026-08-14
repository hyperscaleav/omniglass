import { describe, it, expect } from "vitest";
import { readSettleWindow } from "./command_types";

// readSettleWindow is the one rule the create form and the blade's edit mode both
// read the settle-window field through (#718). The trap it exists to close: the
// console used to fold the typed field with `Number(settle()) || 0`, so an
// untouched field posted a real 0. Beside a target arm that is a settleable type
// judged at the instant of issue, which is a legitimate setting (ADR-0108) but
// never one the operator chose. Unstated must therefore stay distinguishable from
// zero all the way to the wire.
describe("readSettleWindow", () => {
  it("leaves an unstated window unstated on a fire-and-forget type, sending nothing", () => {
    const w = readSettleWindow("", false);
    expect(w.seconds).toBeUndefined();
    expect(w.error).toBeNull();
    expect(w.note).toBeNull();
  });

  it("refuses an unstated window on a settleable type", () => {
    const w = readSettleWindow("", true);
    expect(w.seconds).toBeUndefined();
    expect(w.error).toMatch(/settleable command type needs a settle window/i);
  });

  it("treats whitespace as unstated", () => {
    expect(readSettleWindow("   ", true).error).toMatch(/needs a settle window/i);
    expect(readSettleWindow("   ", false).seconds).toBeUndefined();
  });

  it("reads a stated window as its seconds", () => {
    expect(readSettleWindow("15", true)).toEqual({ seconds: 15, error: null, note: null });
    expect(readSettleWindow(" 15 ", true).seconds).toBe(15);
  });

  // A duration has no negative, and the console must say so where the operator is
  // typing rather than leaving it to the server's 422. This is the ONLY rule now:
  // the `min="0"` that sat beside it could never run (both submit paths are JS,
  // the drawer's action bar and the blade's Save being outside any form) and is
  // gone, with the console's whole validation vocabulary following it into
  // TypeScript (#724, ADR-0113).
  it("refuses a negative window on either kind", () => {
    expect(readSettleWindow("-5", true).error).toMatch(/cannot be negative/i);
    expect(readSettleWindow("-5", false).error).toMatch(/cannot be negative/i);
    expect(readSettleWindow("-5", true).seconds).toBeUndefined();
  });

  // `Number("abc") || 0` was 0, so a typo posted a window nobody typed.
  it("refuses a value that is not a whole number of seconds", () => {
    expect(readSettleWindow("abc", true).error).toMatch(/whole number/i);
    expect(readSettleWindow("1.5", true).error).toMatch(/whole number/i);
    expect(readSettleWindow("1e3", false).error).toMatch(/whole number/i);
  });

  // Zero is not refused: ADR-0108 makes a zero window a statement of intent
  // ("settle immediately"), and reboot ships that shape. It is stated, not
  // defaulted, and what it does is stated with it.
  it("accepts a stated zero beside a target and says what it does there", () => {
    const w = readSettleWindow("0", true);
    expect(w.seconds).toBe(0);
    expect(w.error).toBeNull();
    expect(w.note).toMatch(/judged at the moment it is issued/i);
  });

  it("carries no note for a stated zero on a fire-and-forget type", () => {
    expect(readSettleWindow("0", false)).toEqual({ seconds: 0, error: null, note: null });
  });

  it("notes that a fire-and-forget type never consults a window it was given", () => {
    const w = readSettleWindow("15", false);
    expect(w.seconds).toBe(15);
    expect(w.error).toBeNull();
    expect(w.note).toMatch(/never consulted/i);
  });
});
