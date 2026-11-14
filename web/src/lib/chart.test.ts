import { describe, expect, it } from "vitest";
import { bucketLayout, chartLayout, nearestPoint } from "./chart";

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

// A property series holds its value until the next change (#1269): a step at
// the row's ts, never a ramp between two rows.
describe("step mode", () => {
  const opts = { width: 640, height: 160, padLeft: 40, padRight: 12, padY: 18, now, windowMs: 24 * 3600_000, mode: "step" as const };

  it("changes value at the row's own time: a vertical edge at x, no diagonal", () => {
    const l = chartLayout([at(20, 1), at(10, 3), at(2, 2)], opts);
    const [a, b, c] = l.points;
    // Every command after the first is a horizontal hold or a vertical step.
    const cmds = l.linePath.split(" ").map((s) => ({ op: s[0], xy: s.slice(1).split(",").map(Number) }));
    for (let i = 1; i < cmds.length; i++) {
      const [px, py] = cmds[i - 1].xy;
      const [x, y] = cmds[i].xy;
      expect(x === px || y === py).toBe(true);
    }
    // The step to the second value is at the second row's x, from the first value's y.
    expect(l.linePath).toContain(`L${b.x.toFixed(2)},${a.y.toFixed(2)} L${b.x.toFixed(2)},${b.y.toFixed(2)}`);
    expect(l.linePath).toContain(`L${c.x.toFixed(2)},${b.y.toFixed(2)} L${c.x.toFixed(2)},${c.y.toFixed(2)}`);
  });

  it("holds the last value to now, so a value that has not changed still reaches the right edge", () => {
    const l = chartLayout([at(20, 1), at(10, 3)], opts);
    const end = l.linePath.split(" ").pop()!;
    expect(end).toBe(`L${(opts.width - opts.padRight).toFixed(2)},${l.points[1].y.toFixed(2)}`);
  });

  it("leaves line mode as it was: a ramp between two rows", () => {
    const l = chartLayout([at(20, 1), at(10, 3)], { ...opts, mode: "line" });
    expect(l.linePath).toBe(`M${l.points[0].x.toFixed(2)},${l.points[0].y.toFixed(2)} L${l.points[1].x.toFixed(2)},${l.points[1].y.toFixed(2)}`);
  });
});

// The bucketed read's chart (#1269): the min/max band and the avg line per
// bucket, so a one-sample spike survives any zoom.
describe("bucketLayout", () => {
  const res = 3600_000;
  const opts = { width: 640, height: 160, padLeft: 40, padRight: 12, padY: 18, now, windowMs: 24 * res, resolutionMs: res };
  const bucket = (hoursAgo: number, min: number | null, max: number | null, avg: number | null = null, count = 1, last: number | null = avg) => ({
    ts: new Date(now - hoursAgo * res).toISOString(),
    count: min === null ? 0 : count,
    min,
    max,
    avg: avg ?? (min === null || max === null ? null : (min + max) / 2),
    last,
  });

  it("a one-sample spike reaches the top of the domain: the band covers the bucket's max", () => {
    const l = bucketLayout([bucket(5, 5, 5), bucket(4, 5, 400, 5.2, 90, 5), bucket(3, 5, 5)], opts);
    expect(l.yMax).toBeGreaterThan(400);
    const spike = l.points[1];
    // The band rect for that bucket spans from y(400) to y(5): far taller than the avg dot's own height.
    const rects = l.bandPath.split("M").filter(Boolean);
    expect(rects).toHaveLength(3);
    const tops = rects.map((r) => Number(r.split(" ")[0].split(",")[1]));
    expect(Math.min(...tops)).toBeLessThan(spike.y - 40);
  });

  it("an empty bucket draws no band and no point, and the avg line runs across it (the gap rule is #1475)", () => {
    const l = bucketLayout([bucket(6, 4, 6), bucket(5, null, null), bucket(4, 4, 6)], opts);
    expect(l.bandPath.split("M").filter(Boolean)).toHaveLength(2);
    expect(l.linePath.split("M")).toHaveLength(2);
    // One unbroken line: the two avgs, then on to the end dot (#1789).
    expect(l.linePath.split("L")).toHaveLength(3);
    expect(l.points).toHaveLength(2);
  });

  it("the end label reads the newest bucket's last, not its average", () => {
    const l = bucketLayout([bucket(1, 1, 9, 5, 4, 9)], opts);
    expect(l.points[0].value).toBe(5);
    expect(l.endValue).toBe(9);
  });

  // The line's last vertex, and the right edge of every band rect.
  const lineEnd = (path: string) => path.split(/[ML]/).filter(Boolean).at(-1)!.trim().split(",").map(Number);
  const bandRights = (path: string) => [...path.matchAll(/H([\d.]+) V/g)].map((m) => Number(m[1]));

  it("the line ends on the end dot when the newest bucket's last is not its average (#1789)", () => {
    const l = bucketLayout([bucket(3, 1, 9, 5, 4, 5), bucket(1, 1, 9, 5, 4, 9)], opts);
    expect(l.endDot.y).not.toBeCloseTo(l.points[1].y, 1);
    const [ex, ey] = lineEnd(l.linePath);
    expect(ex).toBeCloseTo(l.endDot.x, 1);
    expect(ey).toBeCloseTo(l.endDot.y, 1);
  });

  it("no band runs past the end of the line (#1789)", () => {
    const l = bucketLayout([bucket(3, 1, 9, 5, 4, 5), bucket(1, 1, 9, 5, 4, 9)], opts);
    const [ex] = lineEnd(l.linePath);
    for (const r of bandRights(l.bandPath)) expect(r).toBeLessThanOrEqual(ex + 0.01);
  });

  it("the newest bucket still in progress ends its line at now, the right edge (#1789)", () => {
    const l = bucketLayout([bucket(2, 1, 9, 5, 4, 5), bucket(0.25, 1, 9, 5, 4, 8)], opts);
    expect(l.endDot.x).toBeCloseTo(opts.width - opts.padRight, 1);
    expect(lineEnd(l.linePath)[0]).toBeCloseTo(opts.width - opts.padRight, 1);
  });

  it("an all-empty window draws nothing", () => {
    const l = bucketLayout([bucket(2, null, null), bucket(1, null, null)], opts);
    expect(l.points).toHaveLength(0);
    expect(l.bandPath).toBe("");
    expect(l.linePath).toBe("");
  });

  it("a flat bucket still draws a visible sliver of band", () => {
    const l = bucketLayout([bucket(2, 7, 7), bucket(1, 7, 7)], opts);
    const h = Number(l.bandPath.split("M")[1].match(/V([\d.]+)/)![1]) - Number(l.bandPath.split("M")[1].split(" ")[0].split(",")[1]);
    expect(h).toBeGreaterThanOrEqual(1);
  });
});
