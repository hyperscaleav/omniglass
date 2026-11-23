import { describe, expect, it } from "vitest";
import { gridWindow, hasData } from "./metrics_grid";

// The metrics grid's pure core (#2424): the page range to the window a chart
// draws and the series read that fills it (the routes count `hours` back
// from now, 1 to 720).
const HOUR = 3600_000;
const now = Date.parse("2026-11-23T12:00:00Z");

describe("gridWindow", () => {
  it("a preset ends now and reads its own width at a step that keeps it a few hundred buckets", () => {
    expect(gridWindow({ kind: "preset", preset: "24h" }, now)).toEqual({ from: now - 24 * HOUR, to: now, hours: 24, stepSeconds: 300 });
    expect(gridWindow({ kind: "preset", preset: "1h" }, now)).toEqual({ from: now - HOUR, to: now, hours: 1, stepSeconds: 60 });
  });

  it("an absolute range draws its own window and names no hours: it reads its own from and to", () => {
    const w = gridWindow({ kind: "absolute", from: now - 5 * HOUR, to: now - 2 * HOUR }, now);
    expect(w).toEqual({ from: now - 5 * HOUR, to: now - 2 * HOUR, hours: undefined, stepSeconds: 60 });
  });
});

describe("gridWindow over an older absolute range", () => {
  // The views read answers hours past the raw samples from the hourly trends
  // only at a whole-hour step (#2304): an absolute range reaching back past a
  // day steps in whole hours even when it is narrow.
  it("steps in whole hours once its start is more than a day back", () => {
    expect(gridWindow({ kind: "absolute", from: now - 3 * 24 * HOUR, to: now - 3 * 24 * HOUR + 6 * HOUR }, now).stepSeconds).toBe(3600);
    expect(gridWindow({ kind: "absolute", from: now - 6 * HOUR, to: now }, now).stepSeconds).toBe(60);
  });
});

describe("hasData", () => {
  const b = (count: number) => ({ ts: "2026-11-23T11:00:00Z", count, min: count ? 1 : null, max: count ? 1 : null, avg: count ? 1 : null, last: count ? 1 : null });
  it("is true only when some bucket holds a sample", () => {
    expect(hasData([])).toBe(false);
    expect(hasData([b(0), b(0)])).toBe(false);
    expect(hasData([b(0), b(3)])).toBe(true);
  });
});
