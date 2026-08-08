import { describe, it, expect } from "vitest";
import { hueFor } from "./system_color";

// hueFor is pure: no I/O, no theme, no DOM. Every case here is deterministic
// given a fixed input string, so nothing is flaky.

// Five uuidv7-shaped ids sharing the same 48-bit millisecond timestamp
// prefix (018f2c9a-3b4c-7), the way five systems minted in one devseed run
// would: only the tail after the shared prefix differs.
const SAME_MILLISECOND_UUIDS = [
  "018f2c9a-3b4c-7000-8000-000000000000",
  "018f2c9a-3b4c-7001-8007-9e3779b10000",
  "018f2c9a-3b4c-7002-800e-3c6ef3620000",
  "018f2c9a-3b4c-7003-8015-daa66d130000",
  "018f2c9a-3b4c-7004-801c-78dde6c40000",
];

function circularGap(a: number, b: number): number {
  const d = Math.abs(a - b);
  return Math.min(d, 360 - d);
}

describe("hueFor", () => {
  it("spreads five same-millisecond uuidv7s at least 30 degrees apart", () => {
    // A PREFIX hash would collapse these to nearly the same hue, since only
    // the leading 48 bits (the timestamp) are shared and a prefix hash reads
    // only that. Hashing the whole string is what recovers the spread.
    const hues = SAME_MILLISECOND_UUIDS.map(hueFor);
    for (let i = 0; i < hues.length; i++) {
      for (let j = i + 1; j < hues.length; j++) {
        expect(circularGap(hues[i], hues[j])).toBeGreaterThanOrEqual(30);
      }
    }
  });

  it("never lands in a band the theme's semantic tokens already claim", () => {
    // Sweep a wide range of ids (not just the five above) so this is a real
    // property check, not five lucky points. Reserved bands: error (~358,
    // wrapping through 0-20), warning (25-55), success (143-167), primary
    // (164-184), info (205-235); see web/src/app.css for the source hexes.
    const reserved = (h: number) =>
      (h >= 340 && h < 360) ||
      (h >= 0 && h < 20) ||
      (h >= 25 && h < 55) ||
      (h >= 143 && h < 167) ||
      (h >= 164 && h < 184) ||
      (h >= 205 && h < 235);
    for (let i = 0; i < 2000; i++) {
      const hue = hueFor(`018f2c9a-3b4c-7${i.toString(16).padStart(3, "0")}-8000-${i.toString(16).padStart(12, "0")}`);
      expect(reserved(hue)).toBe(false);
    }
  });

  it("is a pure function of the whole id: two calls with the same id agree", () => {
    expect(hueFor(SAME_MILLISECOND_UUIDS[0])).toBe(hueFor(SAME_MILLISECOND_UUIDS[0]));
  });

  it("depends on more than the shared timestamp prefix", () => {
    // If hueFor secretly only looked at the leading 12 hex chars (the uuidv7
    // timestamp), every id in this fixture would hash identically. It must not.
    const hues = new Set(SAME_MILLISECOND_UUIDS.map(hueFor));
    expect(hues.size).toBe(SAME_MILLISECOND_UUIDS.length);
  });

  it("stays within 0-359", () => {
    for (const id of SAME_MILLISECOND_UUIDS) {
      const hue = hueFor(id);
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
  });
});
