import { describe, it, expect } from "vitest";
import { hueFor } from "./system_color";

// hueFor is pure: no I/O, no theme, no DOM. Every case here is deterministic
// given a fixed input string, so nothing is flaky.
//
// The OKLCH conversion below is written FROM SCRATCH, independently of
// anything system_color.ts does internally: the point of this file is to
// catch the source's reserved-band math going wrong, which it cannot do if
// it shares the source's own numbers. (Task 9 review, finding C2: the prior
// version of this test hardcoded the same six literals the implementation
// used, so a wrong band and a wrong test always agreed with each other.)

// Björn Ottosson's sRGB -> OKLab -> OKLCH conversion
// (https://bottosson.github.io/posts/oklab/), written independently of any
// conversion system_color.ts might use.
function hexToOklchHue(hex: string): number {
  const n = parseInt(hex.replace("#", ""), 16);
  const toLinear = (c: number) => {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  const r = toLinear((n >> 16) & 0xff);
  const g = toLinear((n >> 8) & 0xff);
  const b = toLinear(n & 0xff);
  const l = 0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b;
  const m = 0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b;
  const s = 0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b;
  const l_ = Math.cbrt(l);
  const m_ = Math.cbrt(m);
  const s_ = Math.cbrt(s);
  const A = 1.9779984951 * l_ - 2.4285922050 * m_ + 0.4505937099 * s_;
  const B = 0.0259040371 * l_ + 0.7827717662 * m_ - 0.8086757660 * s_;
  const deg = (Math.atan2(B, A) * 180) / Math.PI;
  return deg < 0 ? deg + 360 : deg;
}

// The theme's five semantic hexes, both themes (web/src/app.css's
// --color-{success,warning,error,primary,info} blocks, plus accent since it
// shares primary's teal family). The CSS that consumes hueFor's output
// renders oklch() (app.css's .og-system-dot, reusing .tag-pill's tokens), so
// the hue that matters is the OKLCH one, not sRGB/HSL: exactly the axis C2
// found wrong (the prior bands were HSL degrees, off by up to ~60-90 degrees
// from where these colours actually sit once converted to OKLCH).
const SEMANTIC_HEXES = [
  "#3ecf8e", // success, dark
  "#16a06b", // success, light
  "#f0b232", // warning, dark
  "#b9791a", // warning, light
  "#f0676b", // error, dark
  "#d6494d", // error, light
  "#21cab9", // primary, both themes
  "#4cd5c6", // accent, dark
  "#0d8d80", // accent, light
  "#5b8def", // info, dark
  "#3567d6", // info, light
];

const SEMANTIC_OKLCH_HUES = SEMANTIC_HEXES.map(hexToOklchHue);

function circularGap(a: number, b: number): number {
  const d = Math.abs(a - b);
  return Math.min(d, 360 - d);
}

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

  // The real property C2 exists to guard: a derived hue must not read as one
  // of the theme's actual rendered colours. Computed independently (this
  // file's own hexToOklchHue over the raw hexes, never importing anything
  // from system_color.ts's own band list), over a wide sweep so a future
  // band regression has nowhere to hide between a handful of lucky points.
  it("never lands within 8 OKLCH degrees of a real semantic colour", () => {
    for (let i = 0; i < 5000; i++) {
      const id = `018f2c9a-3b4c-7${i.toString(16).padStart(3, "0")}-8000-${i.toString(16).padStart(12, "0")}`;
      const hue = hueFor(id);
      for (const semantic of SEMANTIC_OKLCH_HUES) {
        expect(circularGap(hue, semantic)).toBeGreaterThan(8);
      }
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
