// The metrics grid's pure core (#2424): the page's range to the window its
// charts draw and the series read that fills it. No clock of its own: `now`
// is injected, so the window unit-tests without one.
import type { ChartBucket } from "./chart";
import { rangeHours, rangeStepSeconds, resolveRange, type TimeRange } from "./time_range";

export type GridWindow = { from: number; to: number; hours?: number; stepSeconds: number };

// gridWindow is what a chart draws ([from, to) in epoch ms) and how it reads
// it: a preset reads `hours` back from now on the series route; an absolute
// range has no `hours` and reads its own [from, to) through the views
// binding (metricBucketsBetween, #2304). The step keeps either a few hundred
// buckets wide.
export function gridWindow(range: TimeRange, now: number): GridWindow {
  const { from, to } = resolveRange(range, now);
  return { from, to, hours: rangeHours(range), stepSeconds: rangeStepSeconds(to - from) };
}

// hasData is whether any bucket in the window holds a sample: a metric
// without one is marked "no data in range", not drawn as an empty plot.
export function hasData(buckets: readonly ChartBucket[]): boolean {
  return buckets.some((b) => b.count > 0);
}
