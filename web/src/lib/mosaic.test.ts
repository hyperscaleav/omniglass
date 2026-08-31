import { describe, expect, it } from "vitest";
import { fillFor, layoutPx } from "./mosaic";
import { emptyCounts, type Counts } from "./explore_view";

// The mosaic's pure core (#840). The two claims worth testing are the two the
// first renders falsified: that the layout tiles a frame exactly, and that an
// aggregate's colour says how much is wrong rather than only that something is.

// A tiny deterministic generator, so a property test is reproducible and a
// failure can be re-run rather than re-rolled.
function weights(seed: number, n: number, skew = 1): Array<{ item: number; weight: number }> {
  let s = seed >>> 0;
  const next = () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
  return Array.from({ length: n }, (_, i) => ({ item: i, weight: Math.pow(next(), skew) * 100 + 0.5 }));
}

// Every pixel of the frame belongs to exactly one tile: none shared, none left
// over. This is the property that makes seams and overlaps impossible rather
// than merely unobserved.
function coverage(items: Array<{ item: number; weight: number }>, w: number, h: number) {
  const grid = new Uint8Array(w * h);
  let overlap = 0;
  let outside = 0;
  for (const t of layoutPx(items, w, h)) {
    for (let y = t.y; y < t.y + t.h; y++) {
      for (let x = t.x; x < t.x + t.w; x++) {
        if (x < 0 || y < 0 || x >= w || y >= h) { outside++; continue; }
        const i = y * w + x;
        if (grid[i]) overlap++;
        grid[i] = 1;
      }
    }
  }
  let uncovered = 0;
  for (let i = 0; i < grid.length; i++) if (!grid[i]) uncovered++;
  return { overlap, uncovered, outside };
}

describe("the layout tiles a frame exactly", () => {
  it("covers every pixel once, over many shapes and counts", () => {
    for (const [seed, n, w, h] of [
      [1, 3, 400, 200],
      [2, 8, 320, 180],
      [3, 24, 640, 300],
      [4, 48, 260, 420],
      [5, 60, 900, 120],
    ] as const) {
      const { overlap, uncovered, outside } = coverage(weights(seed, n), w, h);
      expect(overlap, `seed ${seed}: overlapping pixels`).toBe(0);
      expect(uncovered, `seed ${seed}: uncovered pixels`).toBe(0);
      expect(outside, `seed ${seed}: pixels outside the frame`).toBe(0);
    }
  });

  it("covers exactly even when one tile dwarfs the rest", () => {
    // The sliver generator: a heavily skewed set is where a naive split leaves
    // gaps and a percentage layout leaves seams. Sub-pixel tiles are dropped
    // rather than widened, so the frame still tiles exactly.
    const skewed = [{ item: 0, weight: 5000 }, ...weights(9, 30).map((x) => ({ ...x, weight: x.weight / 50 }))];
    const { overlap, uncovered, outside } = coverage(skewed, 500, 300);
    expect(overlap).toBe(0);
    expect(uncovered).toBe(0);
    expect(outside).toBe(0);
  });

  it("drops a sub-pixel tile rather than widening it into its neighbour", () => {
    // Widening to a one-pixel minimum is the tempting fix and it is an overlap.
    // The area budget is what keeps such tiles out of here; this only has to
    // fail safe when it does not.
    const withCrumbs = [{ item: 0, weight: 10_000 }, { item: 1, weight: 0.0001 }];
    const placed = layoutPx(withCrumbs, 40, 20);
    expect(placed.every((p) => p.w > 0 && p.h > 0)).toBe(true);
    const { overlap, uncovered } = coverage(withCrumbs, 40, 20);
    expect(overlap).toBe(0);
    expect(uncovered).toBe(0);
  });

  it("keeps tiles near square rather than producing slivers", () => {
    const placed = layoutPx(weights(7, 20), 600, 400);
    const worst = Math.max(...placed.map((p) => Math.max(p.w / p.h, p.h / p.w)));
    expect(worst).toBeLessThan(8);
  });

  it("never emits a zero-sized tile", () => {
    for (const p of layoutPx(weights(11, 40), 200, 120)) {
      expect(p.w).toBeGreaterThanOrEqual(1);
      expect(p.h).toBeGreaterThanOrEqual(1);
    }
  });

  it("returns nothing for an empty set or a frame with no room", () => {
    expect(layoutPx([], 100, 100)).toEqual([]);
    expect(layoutPx(weights(1, 4), 0, 100)).toEqual([]);
    expect(layoutPx([{ item: 0, weight: 0 }], 100, 100)).toEqual([]);
  });
});

describe("aggregate colour is a share, not a rollup", () => {
  const counts = (healthy: number, degraded: number, outage: number): Counts => ({
    ...emptyCounts(),
    healthy,
    degraded,
    outage,
  });

  it("does not saturate: one outage in sixty reads differently from twenty", () => {
    // This is the regression test against worst-wins. Both aggregates contain
    // an outage, so a rollup would paint them identically.
    const one = fillFor(counts(59, 0, 1));
    const many = fillFor(counts(40, 0, 20));
    expect(one.severity).toBe("outage");
    expect(many.severity).toBe("outage");
    expect(many.share).toBeGreaterThan(one.share * 3);
  });

  it("still shows a lone bad room rather than rounding it away", () => {
    // The opposite failure to saturation: a linear share would make 1.7%
    // invisible.
    expect(fillFor(counts(59, 0, 1)).share).toBeGreaterThan(0.1);
  });

  it("takes its hue from the worst thing present", () => {
    expect(fillFor(counts(10, 5, 0)).severity).toBe("degraded");
    expect(fillFor(counts(10, 5, 1)).severity).toBe("outage");
  });

  it("reads healthy as no share at all, not as a faint wash", () => {
    expect(fillFor(counts(12, 0, 0))).toEqual({ severity: "healthy", share: 0 });
  });

  it("treats an empty aggregate as idle rather than healthy", () => {
    // A card holding nothing has not passed a check; it has had none.
    expect(fillFor(emptyCounts())).toEqual({ severity: "idle", share: 0 });
  });

  it("does not count incomplete as attention, so commissioning is not a fault", () => {
    const c: Counts = { ...emptyCounts(), healthy: 5, incomplete: 5 };
    expect(fillFor(c)).toEqual({ severity: "healthy", share: 0 });
  });

  it("reaches full share when everything is wrong", () => {
    expect(fillFor(counts(0, 0, 8)).share).toBe(1);
  });
});
