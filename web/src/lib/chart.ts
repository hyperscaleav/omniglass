// The timeseries chart's pure core (#794): sample rows to plot geometry, no
// DOM and no clock of its own. The impure shell (TimeseriesChart) only draws
// what this returns, so the geometry unit-tests without a browser. Built here
// for the Data tab and reused by dashboards later (primitive first).

export type ChartSample = { ts: string; value: number };
export type ChartPoint = { x: number; y: number; ts: string; value: number };

export type ChartOpts = {
  width: number;
  height: number;
  padLeft: number;
  padRight: number;
  padY: number;
  now: number;
  windowMs: number;
};

export type ChartLayout = {
  points: ChartPoint[];
  yMin: number;
  yMax: number;
  yTicks: number[];
  // The SVG path strings the shell draws: the line, and the area closed to
  // the baseline.
  linePath: string;
  areaPath: string;
};

export function chartLayout(samples: ChartSample[], o: ChartOpts): ChartLayout {
  const rows = [...samples]
    .map((s) => ({ t: Date.parse(s.ts), value: s.value, ts: s.ts }))
    .filter((s) => Number.isFinite(s.t))
    .sort((a, b) => a.t - b.t);
  if (rows.length === 0) return { points: [], yMin: 0, yMax: 1, yTicks: [], linePath: "", areaPath: "" };

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
  const linePath = points.map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(" ");
  const base = (o.height - o.padY).toFixed(2);
  const areaPath = `${linePath} L${points[points.length - 1].x.toFixed(2)},${base} L${points[0].x.toFixed(2)},${base} Z`;

  // Three ticks: bottom, middle, top of the padded domain, rounded for the
  // label without leaving the domain.
  const yTicks = [lo, (lo + hi) / 2, hi];
  return { points, yMin: lo, yMax: hi, yTicks, linePath, areaPath };
}
