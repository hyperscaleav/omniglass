import { describe, it, expect } from "vitest";
import { PNG } from "pngjs";
import { compareShot, DEFAULT_MAX_RATIO } from "../e2e/shotdiff.mjs";

// The freshness gate's blindness class (#398, #623): a renamed label repaints a
// few dozen glyph pixels, the same order of magnitude as the old seed-noise
// tolerance absorbed, so the gate passed exactly the changes it exists to catch
// (0.033% on a renamed Scope column, 0.13% on a collapsed rail, both under the
// 0.5% ceiling). With every nondeterministic region masked at capture, the
// tolerance's reason is gone: the shipped default is zero, and ANY unmasked
// pixel change fails.
function png(width: number, height: number, paint?: (data: Buffer) => void): Buffer {
  const img = new PNG({ width, height });
  img.data.fill(255);
  paint?.(img.data);
  return PNG.sync.write(img);
}

const W = 400, H = 200;
const base = png(W, H);
// A few-glyph label rename: 60 of 80,000 pixels (0.075%), the #398 scale.
const renamed = png(W, H, (d) => {
  for (let i = 0; i < 60; i++) d[(20 * W + 100 + i) * 4] = 0;
});

describe("shot freshness gate", () => {
  it("ships a zero tolerance, so any unmasked pixel change fails", () => {
    expect(DEFAULT_MAX_RATIO).toBe(0);
    const r = compareShot(base, renamed, { maxRatio: DEFAULT_MAX_RATIO });
    expect(r.status).toBe("changed");
  });

  it("documents the old blindness: the same rename passed the 0.5% tolerance", () => {
    const r = compareShot(base, renamed, { maxRatio: 0.005 });
    if (r.status === "size") throw new Error("unexpected size mismatch");
    expect(r.status).toBe("ok");
    expect(r.ratio).toBeGreaterThan(0);
    expect(r.ratio).toBeLessThan(0.005);
  });

  it("passes an identical capture at zero", () => {
    const r = compareShot(base, png(W, H), { maxRatio: 0 });
    if (r.status === "size") throw new Error("unexpected size mismatch");
    expect(r.status).toBe("ok");
    expect(r.ratio).toBe(0);
  });

  it("reports a size change as its own failure, not a ratio", () => {
    const r = compareShot(base, png(W + 2, H), { maxRatio: 0 });
    expect(r.status).toBe("size");
  });
});
