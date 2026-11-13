// The timeseries chart's pure core (#794): sample rows to plot geometry, no
// DOM and no clock of its own. The impure shell (TimeseriesChart) only draws
// what this returns, so the geometry unit-tests without a browser. Built here
// for the Data tab and reused by dashboards later (primitive first).

export type ChartSample = { ts: string; value: number };
// One bucket of the bucketed series read (#1269): an empty bucket has count 0
// and null aggregates, so no data is never drawn as a zero.
export type ChartBucket = { ts: string; count: number; min: number | null; max: number | null; avg: number | null; last: number | null };
// A bucket's point carries its band, so the crosshair reads the spike and not
// only the average.
export type ChartPoint = { x: number; y: number; ts: string; value: number; min?: number; max?: number; count?: number };

export type ChartOpts = {
  width: number;
  height: number;
  padLeft: number;
  padRight: number;
  padY: number;
  now: number;
  windowMs: number;
  // How the line joins two rows: a metric ramps between samples ("line", the
  // default), a property holds its value until the next change ("step",
  // #1269).
  mode?: "line" | "step";
};

export type BucketOpts = ChartOpts & { resolutionMs: number };

export type ChartLayout = {
  points: ChartPoint[];
  yMin: number;
  yMax: number;
  yTicks: number[];
  // The SVG path strings the shell draws: the line, and the area closed to
  // the baseline.
  linePath: string;
  areaPath: string;
  // Where the newest sample's value floats: centered above its dot,
  // intersecting the line's territory, clamped into the frame.
  endLabel: { x: number; y: number };
  // Four time ticks spanning the window, oldest at the left plot edge and
  // now at the right: the x axis (#795 review; a chart with no time axis
  // says nothing about timing).
  xTicks: { x: number; ts: string }[];
  // The min/max band of a bucketed series: one rect subpath per non-empty
  // bucket; "" for a raw series.
  bandPath: string;
  // The value the end label and dot show: the newest sample, or a bucket's
  // last. Absent when nothing was read.
  endValue?: number;
  // Where the end dot sits: the newest sample's point, or at a bucketed
  // series' newest bucket the height of its last value.
  endDot: { x: number; y: number };
};

export function chartLayout(samples: ChartSample[], o: ChartOpts): ChartLayout {
  const rows = [...samples]
    .map((s) => ({ t: Date.parse(s.ts), value: s.value, ts: s.ts }))
    .filter((s) => Number.isFinite(s.t))
    .sort((a, b) => a.t - b.t);
  const xTicks = [0, 1 / 3, 2 / 3, 1].map((f) => ({
    x: o.padLeft + f * (o.width - o.padLeft - o.padRight),
    ts: new Date(o.now - (1 - f) * o.windowMs).toISOString(),
  }));
  if (rows.length === 0) return emptyLayout(xTicks);

  let lo = Math.min(...rows.map((r) => r.value));
  let hi = Math.max(...rows.map((r) => r.value));
  // A padded domain: a flat series draws mid-frame, never on the edge.
  const span = hi - lo || Math.abs(hi) * 0.1 || 1;
  lo -= span * 0.15;
  hi += span * 0.15;

  const plotW = o.width - o.padLeft - o.padRight;
  const plotH = o.height - o.padY * 2;
  const x = (t: number) => o.padLeft + Math.min(Math.max(1 - (o.now - t) / o.windowMs, 0), 1) * plotW;
  const y = (v: number) => o.padY + (1 - (v - lo) / (hi - lo)) * plotH;

  const points: ChartPoint[] = rows.map((r) => ({ x: x(r.t), y: y(r.value), ts: r.ts, value: r.value }));
  const linePath = o.mode === "step" ? stepPath(points, x(o.now)) : points.map((p, i) => `${i === 0 ? "M" : "L"}${f2(p.x)},${f2(p.y)}`).join(" ");
  const base = (o.height - o.padY).toFixed(2);
  const areaEnd = o.mode === "step" ? x(o.now) : points[points.length - 1].x;
  const areaPath = `${linePath} L${f2(areaEnd)},${base} L${f2(points[0].x)},${base} Z`;

  // Three ticks: bottom, middle, top of the padded domain, rounded for the
  // label without leaving the domain.
  const yTicks = [lo, (lo + hi) / 2, hi];
  const end = points[points.length - 1];
  const endLabel = {
    x: Math.min(Math.max(end.x, o.padLeft + 14), o.width - o.padRight),
    y: Math.max(end.y - 9, 9),
  };
  return { points, yMin: lo, yMax: hi, yTicks, linePath, areaPath, bandPath: "", endLabel, endValue: end.value, endDot: { x: end.x, y: end.y }, xTicks };
}

const f2 = (n: number) => n.toFixed(2);

function emptyLayout(xTicks: ChartLayout["xTicks"]): ChartLayout {
  return { points: [], yMin: 0, yMax: 1, yTicks: [], linePath: "", areaPath: "", bandPath: "", endLabel: { x: 0, y: 0 }, endDot: { x: 0, y: 0 }, xTicks };
}

// stepPath holds each row's value until the next row's own x, then changes it
// there with a vertical edge, and holds the last value out to the right edge:
// an observed value is in force from its ts, never ramped toward the next.
function stepPath(points: ChartPoint[], xEnd: number): string {
  const cmds = [`M${f2(points[0].x)},${f2(points[0].y)}`];
  for (let i = 1; i < points.length; i++) {
    cmds.push(`L${f2(points[i].x)},${f2(points[i - 1].y)}`, `L${f2(points[i].x)},${f2(points[i].y)}`);
  }
  const last = points[points.length - 1];
  cmds.push(`L${f2(Math.max(xEnd, last.x))},${f2(last.y)}`);
  return cmds.join(" ");
}

// bucketLayout is chartLayout for the bucketed read (#1269): each non-empty
// bucket draws its min..max as a band rect (never thinner than a pixel, so a
// flat bucket still shows) and the avg joins into the line. An empty bucket
// draws no band and no point, so a spike survives any zoom; the line runs
// across it, as the raw line ran between samples, because breaking it where a
// series stopped is the cadence-derived gap rule (#1475).
export function bucketLayout(buckets: ChartBucket[], o: BucketOpts): ChartLayout {
  const xTicks = [0, 1 / 3, 2 / 3, 1].map((f) => ({
    x: o.padLeft + f * (o.width - o.padLeft - o.padRight),
    ts: new Date(o.now - (1 - f) * o.windowMs).toISOString(),
  }));
  const rows = buckets
    .map((b) => ({ ...b, t: Date.parse(b.ts) }))
    .filter((b) => Number.isFinite(b.t))
    .sort((a, b) => a.t - b.t);
  const full = rows.filter((b) => b.count > 0 && b.min !== null && b.max !== null && b.avg !== null);
  if (full.length === 0) return emptyLayout(xTicks);

  let lo = Math.min(...full.map((b) => b.min as number));
  let hi = Math.max(...full.map((b) => b.max as number));
  const span = hi - lo || Math.abs(hi) * 0.1 || 1;
  lo -= span * 0.15;
  hi += span * 0.15;

  const plotW = o.width - o.padLeft - o.padRight;
  const plotH = o.height - o.padY * 2;
  const x = (t: number) => o.padLeft + Math.min(Math.max(1 - (o.now - t) / o.windowMs, 0), 1) * plotW;
  const y = (v: number) => o.padY + (1 - (v - lo) / (hi - lo)) * plotH;

  const points: ChartPoint[] = [];
  let linePath = "";
  let bandPath = "";
  for (const b of rows) {
    if (b.count <= 0 || b.min === null || b.max === null || b.avg === null) {
      continue;
    }
    const x0 = x(b.t);
    const x1 = Math.max(x(b.t + o.resolutionMs), x0 + 0.75);
    const yTop = y(b.max);
    const yBot = Math.max(y(b.min), yTop + 1);
    bandPath += `${bandPath ? " " : ""}M${f2(x0)},${f2(yTop)} H${f2(x1)} V${f2(yBot)} H${f2(x0)} Z`;
    const p: ChartPoint = { x: x(b.t + o.resolutionMs / 2), y: y(b.avg), ts: b.ts, value: b.avg, min: b.min, max: b.max, count: b.count };
    linePath += `${linePath ? " L" : "M"}${f2(p.x)},${f2(p.y)}`;
    points.push(p);
  }
  // The newest bucket's last is the newest reading, taken at the bucket's end
  // (or now, for the bucket still filling): the line runs on from its avg to
  // that dot, so the dot sits on the line and no band runs past it (#1789).
  const newest = full[full.length - 1];
  const endValue = newest.last ?? (newest.avg as number);
  const endX = x(Math.min(newest.t + o.resolutionMs, o.now));
  const endY = y(endValue);
  linePath += ` L${f2(endX)},${f2(endY)}`;
  return {
    points,
    yMin: lo,
    yMax: hi,
    yTicks: [lo, (lo + hi) / 2, hi],
    linePath,
    // No area under the avg line: the band is a bucketed chart's fill.
    areaPath: "",
    bandPath,
    endLabel: { x: Math.min(Math.max(endX, o.padLeft + 14), o.width - o.padRight), y: Math.max(endY - 9, 9) },
    endValue,
    endDot: { x: endX, y: endY },
    xTicks,
  };
}

// nearestPoint resolves a plot-space x to the closest SAMPLE, never an
// interpolation: the crosshair reads what was measured, and between two
// samples it snaps to the nearer one.
export function nearestPoint(l: ChartLayout, x: number): ChartPoint | null {
  let best: ChartPoint | null = null;
  let bestDist = Infinity;
  for (const p of l.points) {
    const d = Math.abs(p.x - x);
    if (d < bestDist) {
      bestDist = d;
      best = p;
    }
  }
  return best;
}
