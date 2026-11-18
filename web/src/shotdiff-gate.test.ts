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

  // The gate's zero is a zero count of pixels that differ in any channel (#774).
  // It used to be a zero count of pixels pixelmatch judges different at threshold
  // 0.1, which cannot see a change confined to the console's dark surface tones
  // (#0d1117 against #0c1320 scores 0): a mask edge moving over a dark surface
  // scored 0.000% (#1081: 64 and 160 changed pixels), and a mask box resizing
  // with the clock moved 8,400 pixels the gate passed (#773).
  describe("the exact default", () => {
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

    // The old mask fill (#0d1117) against the card row (#0c1320): one step
    // apart in the console's tokens, invisible to pixelmatch at 0.1.
    const fill = (rgb: number[]) =>
      png(W, H, (d) => {
        for (let i = 0; i < d.length; i += 4) {
          d[i] = rgb[0]; d[i + 1] = rgb[1]; d[i + 2] = rgb[2];
        }
      });
    const patch = (rgb: number[]) =>
      png(W, H, (d) => {
        for (let i = 0; i < d.length; i += 4) {
          d[i] = 12; d[i + 1] = 19; d[i + 2] = 32;
        }
        for (let y = 50; y < 60; y++)
          for (let x = 10; x < 40; x++) {
            const i = (y * W + x) * 4;
            d[i] = rgb[0]; d[i + 1] = rgb[1]; d[i + 2] = rgb[2];
          }
      });

    it("fails a #0d1117 against #0c1320 difference, counting the pixels exactly", () => {
      const r = compareShot(patch([12, 19, 32]), patch([13, 17, 23]));
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("changed");
      expect(r.diffPixels).toBe(300);
      expect(r.ratio).toBe(300 / (W * H));
    });

    it("fails a whole-surface swap between the two tones", () => {
      const r = compareShot(fill([13, 17, 23]), fill([12, 19, 32]));
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("changed");
      expect(r.diffPixels).toBe(W * H);
    });

    it("fails a mask edge moved one pixel, counting every pixel that differs", () => {
      const r = compareShot(masked(200), masked(201));
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("changed");
      expect(r.diffPixels).toBe(20);
    });

    it("fails a difference in the alpha channel alone", () => {
      const r = compareShot(base, png(W, H, (d) => { d[3] = 254; }));
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("changed");
      expect(r.diffPixels).toBe(1);
    });

    it("applies maxRatio, the unpinned-browser escape hatch, to the exact count", () => {
      const r = compareShot(masked(200), masked(201), { maxRatio: 0.001 });
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("ok");
      expect(r.diffPixels).toBe(20);
      expect(compareShot(masked(200), masked(201), { maxRatio: 0.0001 }).status).toBe("changed");
    });

    it("returns ok for byte-identical buffers without decoding them", () => {
      // Not a PNG: a decode would throw, so an ok result proves none happened.
      const bytes = Buffer.from("not a png, but the same bytes");
      const r = compareShot(bytes, Buffer.from(bytes));
      expect(r).toEqual({ status: "ok", ratio: 0, diffPixels: 0 });
    });

    it("passes rasters that are pixel-equal but not byte-equal", () => {
      const a = masked(200);
      const img = PNG.sync.read(a);
      const b = PNG.sync.write(img, { deflateLevel: 1, filterType: 0 });
      expect(Buffer.compare(a, b)).not.toBe(0);
      const r = compareShot(a, b);
      if (r.status === "size") throw new Error("unexpected size mismatch");
      expect(r.status).toBe("ok");
      expect(r.diffPixels).toBe(0);
    });

    it("still reports a size change as its own failure", () => {
      expect(compareShot(base, png(W + 2, H)).status).toBe("size");
    });
  });
});
