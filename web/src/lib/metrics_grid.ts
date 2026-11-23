// The metrics grid's pure core (#2424): the page's range to the window its
// charts draw and the series read that fills it. No clock of its own: `now`
// is injected, so the window unit-tests without one.
import type { ChartBucket } from "./chart";
import { rangeHours, rangeStepSeconds, resolveRange, type TimeRange } from "./time_range";

const HOUR = 3600_000;
// The series routes count `hours` back from now, 1 to 720 (30 days).
const MAX_HOURS = 720;

export type GridWindow = { from: number; to: number; hours: number; stepSeconds: number };

// gridWindow is what a chart draws ([from, to) in epoch ms) and what it reads
// (`hours` back from now at `stepSeconds` a bucket). An absolute range has no
// width of its own on those routes, so it reads back far enough to cover its
// start (capped at 720) and the chart keeps only its own window.
export function gridWindow(range: TimeRange, now: number): GridWindow {
  const { from, to } = resolveRange(range, now);
  const hours = rangeHours(range) ?? Math.min(Math.max(Math.ceil((now - from) / HOUR), 1), MAX_HOURS);
  return { from, to, hours, stepSeconds: rangeStepSeconds(to - from) };
}

// hasData is whether any bucket in the window holds a sample: a metric
// without one is marked "no data in range", not drawn as an empty plot.
export function hasData(buckets: readonly ChartBucket[]): boolean {
  return buckets.some((b) => b.count > 0);
}
