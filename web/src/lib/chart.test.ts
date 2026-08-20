import { describe, expect, it } from "vitest";
import { chartLayout } from "./chart";

// The timeseries chart's pure core (#794): samples to plot geometry, no DOM.
// X is time against the window's right-edge-now axis; Y is a padded nice
// domain so a flat series never draws on the frame's edge.
const now = Date.parse("2026-08-20T17:00:00Z");
const at = (hoursAgo: number, value: number) => ({ ts: new Date(now - hoursAgo * 3600_000).toISOString(), value });

describe("chartLayout", () => {
  const opts = { width: 640, height: 160, padLeft: 40, padRight: 12, padY: 18, now, windowMs: 24 * 3600_000 };

  it("maps time onto x (newest at the right edge) and value onto y (larger is higher)", () => {
    const l = chartLayout([at(24, 5), at(12, 6), at(0, 7)], opts);
    expect(l.points).toHaveLength(3);
    expect(l.points[0].x).toBeCloseTo(40, 3);
    expect(l.points[2].x).toBeCloseTo(628, 3);
    expect(l.points[2].y).toBeLessThan(l.points[0].y);
    expect(l.points.map((p) => p.x)).toEqual([...l.points.map((p) => p.x)].sort((a, b) => a - b));
  });

  it("pads a flat series so the line never sits on the frame", () => {
    const l = chartLayout([at(2, 6.1), at(1, 6.1)], opts);
    expect(l.yMin).toBeLessThan(6.1);
    expect(l.yMax).toBeGreaterThan(6.1);
    expect(l.points[0].y).toBeGreaterThan(opts.padY);
    expect(l.points[0].y).toBeLessThan(opts.height - opts.padY);
  });

  it("orders unordered samples by time and emits ticks inside the domain", () => {
    const l = chartLayout([at(0, 7), at(24, 5)], opts);
    expect(l.points[0].value).toBe(5);
    for (const t of l.yTicks) {
      expect(t).toBeGreaterThanOrEqual(l.yMin);
      expect(t).toBeLessThanOrEqual(l.yMax);
    }
    expect(chartLayout([], opts).points).toHaveLength(0);
  });
});
