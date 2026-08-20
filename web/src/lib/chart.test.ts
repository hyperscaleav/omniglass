import { describe, expect, it } from "vitest";
import { chartLayout, nearestPoint } from "./chart";

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

describe("the endpoint label", () => {
  const opts = { width: 640, height: 160, padLeft: 40, padRight: 12, padY: 18, now, windowMs: 24 * 3600_000 };

  it("floats centered above the newest sample's dot, intersecting the line's territory", () => {
    const l = chartLayout([at(4, 5), at(0, 6.1)], opts);
    const end = l.points[l.points.length - 1];
    expect(l.endLabel.x).toBeCloseTo(end.x, 5);
    expect(l.endLabel.y).toBeLessThan(end.y);
    expect(end.y - l.endLabel.y).toBeLessThanOrEqual(14);
  });

  it("clamps into the frame when the newest sample sits at an edge", () => {
    const l = chartLayout([at(0.1, 100)], opts);
    expect(l.endLabel.x).toBeLessThanOrEqual(opts.width - opts.padRight);
    expect(l.endLabel.y).toBeGreaterThanOrEqual(9);
  });
});

describe("the x axis and the crosshair (#795 review)", () => {
  const opts = { width: 640, height: 160, padLeft: 40, padRight: 12, padY: 18, now, windowMs: 24 * 3600_000 };

  it("emits four x ticks spanning the window, oldest at the left edge and now at the right", () => {
    const l = chartLayout([at(20, 5), at(0, 6)], opts);
    expect(l.xTicks).toHaveLength(4);
    expect(l.xTicks[0].x).toBeCloseTo(40, 3);
    expect(l.xTicks[3].x).toBeCloseTo(628, 3);
    expect(Date.parse(l.xTicks[3].ts) - Date.parse(l.xTicks[0].ts)).toBe(24 * 3600_000);
  });

  it("nearestPoint resolves a plot x to the closest SAMPLE, never an interpolation", () => {
    const l = chartLayout([at(24, 5), at(12, 6), at(0, 7)], opts);
    const mid = l.points[1];
    expect(nearestPoint(l, mid.x + 3)).toBe(mid);
    expect(nearestPoint(l, 0)).toBe(l.points[0]);
    expect(nearestPoint(chartLayout([], opts), 100)).toBeNull();
  });
});
