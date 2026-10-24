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

  // The gate's zero is a zero COUNT of pixels pixelmatch judges different at
  // threshold 0.1 (#774), and a mask edge moving over the console's dark surface
  // is below that threshold: #1081's since-line masks moved a column of pixels
  // (64 and 160 changed) and the gate scored both 0.000%. Proving a mask holds
  // needs the exact comparison, opted into with DOCS_SHOTS_EXACT=1.
  describe("the exact comparison", () => {
    const SURFACE = [12, 19, 32], MASK = [26, 35, 54];
    // A dark card with a mask panel whose right edge sits at `edge`.
    const masked = (edge: number) =>
      png(W, H, (d) => {
        for (let y = 0; y < H; y++)
          for (let x = 0; x < W; x++) {
            const [r, g, b] = x >= 40 && x < edge && y >= 80 && y < 100 ? MASK : SURFACE;
            const i = (y * W + x) * 4;
            d[i] = r; d[i + 1] = g; d[i + 2] = b;
          }
      });

    it("documents the blindness: a mask edge moved one pixel scores zero by default", () => {
      const r = compareShot(masked(200), masked(201), { maxRatio: 0 });
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("ok");
      expect(r.diffPixels).toBe(0);
    });

    it("fails the same move, counting every pixel that differs at all", () => {
      const r = compareShot(masked(200), masked(201), { exact: true });
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("changed");
      expect(r.diffPixels).toBe(20);
    });

    it("passes an identical capture, and ignores the ratio it was not asked for", () => {
      const r = compareShot(masked(200), masked(200), { exact: true, maxRatio: 0.5 });
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("ok");
      expect(r.diffPixels).toBe(0);
    });

    it("still reports a size change as its own failure", () => {
      expect(compareShot(base, png(W + 2, H), { exact: true }).status).toBe("size");
    });
  });
});
