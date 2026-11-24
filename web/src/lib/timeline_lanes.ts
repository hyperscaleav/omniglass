// The overlay timeline's pure core (#2425): the reads a component or a system
// answers over the page's range, turned into the lanes TimelineOverlay draws on
// one time axis. Health transitions become verdict spans, alarms become spans
// that know how long they were raised, property changes become value bands,
// events, log lines and commands become markers (clustered where they crowd),
// and a metric becomes a line. No DOM, no clock of its own: every function
// takes the range and the instant it calls now, so each edge case unit-tests.
//
// Spans are always clipped to [from, to] and to now: a stretch nobody has lived
// through yet is never drawn. What a span says about itself (an alarm's
// duration) is never clipped: an alarm raised before the range opened was
// still raised for its whole length.
import { bucketLayout, xAtTime, type ChartBucket, type ChartLayout } from "./chart";
import { durationText } from "./timeline";

export type TimeWindow = { from: number; to: number };

// The horizontal frame a lane is drawn in: the plot runs between the pads.
export type LaneFrame = { width: number; padLeft: number; padRight: number };

// xOf places an instant on the plot, clamped to its edges.
export function xOf(t: number, r: TimeWindow, f: LaneFrame): number {
  return xAtTime(t, { ...f, now: r.to, windowMs: Math.max(r.to - r.from, 1) });
}

const ms = (iso: string | undefined | null): number | undefined => {
  if (!iso) return undefined;
  const t = Date.parse(iso);
  return Number.isFinite(t) ? t : undefined;
};

// A stretch of one value: a health verdict or a property's value.
export type ValueSpan = { value: string; from: number; to: number };

// stretches is the shared walk behind health and property lanes: the value in
// force at the range's start (the last change at or before it, else the
// carried-in value), then one stretch per change inside the range, merging a
// change to the value already held, each running to the next change or to the
// range's end (never past now).
function stretches(changes: { t: number; value: string }[], carried: string | null | undefined, r: TimeWindow, now: number): ValueSpan[] {
  const end = Math.min(r.to, now);
  if (end <= r.from) return [];
  const sorted = changes.filter((c) => Number.isFinite(c.t)).sort((a, b) => a.t - b.t);
  let held: string | undefined = carried ?? undefined;
  const inside: { t: number; value: string }[] = [];
  for (const c of sorted) {
    if (c.t <= r.from) held = c.value;
    else if (c.t < end) inside.push(c);
  }
  const out: ValueSpan[] = [];
  let cur: ValueSpan | undefined = held !== undefined ? { value: held, from: r.from, to: end } : undefined;
  for (const c of inside) {
    if (cur && cur.value === c.value) continue;
    if (cur) {
      cur.to = c.t;
      out.push(cur);
    }
    cur = { value: c.value, from: c.t, to: end };
  }
  if (cur) out.push(cur);
  return out;
}

// healthSpans turns recorded health edges into verdict spans over the range.
// carriedIn is the verdict in force when the range opens (the health read's
// carried_in). Without it the stretch before the first edge is unknown and left
// undrawn, except that a range reaching now with no edge at all is the current
// verdict, held: nothing changed, so what it is now is what it was.
export function healthSpans(
  transitions: readonly { ts: string; health: string }[],
  r: TimeWindow,
  o: { carriedIn?: string | null; current?: string | null; now: number },
): ValueSpan[] {
  const changes = transitions.map((t) => ({ t: ms(t.ts) ?? NaN, value: t.health }));
  let carried = o.carriedIn ?? undefined;
  if (carried === undefined && changes.length === 0 && r.to >= o.now && o.current) carried = o.current;
  return stretches(changes, carried, r, o.now);
}

// propertyBands is a property's value bands: the value carried in at the
// range's start, then each change, held until the next.
export function propertyBands(points: readonly { ts: string; value: string }[], carriedIn: string | null | undefined, r: TimeWindow, now: number): ValueSpan[] {
  return stretches(
    points.map((p) => ({ t: ms(p.ts) ?? NaN, value: p.value })),
    carriedIn,
    r,
    now,
  );
}

// spanAt is the span in force at an instant: what the cursor reads on a lane.
export function spanAt<S extends { from: number; to: number }>(list: readonly S[], t: number): S | undefined {
  return list.find((s) => t >= s.from && t < s.to) ?? (list.length && t === list[list.length - 1].to ? list[list.length - 1] : undefined);
}

// The alarm fields a span reads: the list rows of the component and system
// alarm reads.
export type AlarmRow = {
  id: string;
  component: string;
  component_id?: string;
  severity: string;
  message: string;
  raised_by_rule?: string;
  raised_by_rule_id?: string;
  raised_at: string;
  cleared_at?: string;
  acknowledged: boolean;
  acknowledged_by?: string;
};

export type AlarmSpan = {
  id: string;
  component: string;
  componentId?: string;
  severity: string;
  message: string;
  rule?: string;
  ruleId?: string;
  acknowledged: boolean;
  acknowledgedBy?: string;
  raisedAt: number;
  clearedAt?: number;
  // The drawn stretch, clipped to the range and to now.
  from: number;
  to: number;
  // Raised for this long: clear minus raise, or now minus raise while open.
  duration: number;
  // Still raised at the range's end (or now).
  open: boolean;
  // Raised before the range opened.
  clippedStart: boolean;
  // Its row on the Alarms lane: one per alarm, oldest raise first.
  row: number;
};

// alarmSpans keeps the alarms whose raised stretch overlaps the range, one row
// each, oldest raise first, so overlapping alarms read as a stack.
export function alarmSpans(alarms: readonly AlarmRow[], r: TimeWindow, now: number): AlarmSpan[] {
  const end = Math.min(r.to, now);
  const out: AlarmSpan[] = [];
  for (const a of alarms) {
    const raisedAt = ms(a.raised_at);
    if (raisedAt === undefined) continue;
    const clearedAt = ms(a.cleared_at);
    const stop = clearedAt ?? now;
    if (raisedAt >= end || stop <= r.from) continue;
    out.push({
      id: a.id,
      component: a.component,
      componentId: a.component_id,
      severity: a.severity,
      message: a.message,
      rule: a.raised_by_rule,
      ruleId: a.raised_by_rule_id,
      acknowledged: a.acknowledged,
      acknowledgedBy: a.acknowledged_by,
      raisedAt,
      clearedAt,
      from: Math.max(raisedAt, r.from),
      to: Math.min(stop, end),
      duration: Math.max(stop - raisedAt, 0),
      open: clearedAt === undefined || clearedAt > end,
      clippedStart: raisedAt < r.from,
      row: 0,
    });
  }
  out.sort((a, b) => a.raisedAt - b.raisedAt || a.id.localeCompare(b.id));
  out.forEach((s, i) => (s.row = i));
  return out;
}

// alarmTip is what hovering an alarm span says: what it is, which rule, when it
// was raised and cleared, and for how long. fmt renders an instant.
export function alarmTip(s: AlarmSpan, fmt: (iso: string) => string): string {
  const iso = (t: number) => fmt(new Date(t).toISOString());
  const parts = [`${s.severity} · ${s.message}`];
  if (s.rule) parts.push(`rule ${s.rule}`);
  parts.push(`raised ${iso(s.raisedAt)}`);
  if (s.clearedAt !== undefined) parts.push(`cleared ${iso(s.clearedAt)}`, `raised for ${durationText(s.duration)}`);
  else parts.push(`still raised, ${durationText(s.duration)} so far`);
  parts.push(s.acknowledged ? `acknowledged${s.acknowledgedBy ? ` by ${s.acknowledgedBy}` : ""}` : "not acknowledged");
  return parts.join(" · ");
}

// valueSpanTip is a verdict or value band's hover: the value, its bounds, and
// how long it held.
export function valueSpanTip(s: ValueSpan, fmt: (iso: string) => string, now: number): string {
  const live = s.to >= now;
  const from = fmt(new Date(s.from).toISOString());
  return live ? `${s.value} since ${from} · ${durationText(s.to - s.from)} so far` : `${s.value} from ${from} to ${fmt(new Date(s.to).toISOString())} · held ${durationText(s.to - s.from)}`;
}

export type Marker<T> = { t: number; item: T };

// markersInRange keeps the rows stamped inside [from, to], oldest first.
export function markersInRange<T>(items: readonly T[], ts: (x: T) => string | undefined, r: TimeWindow): Marker<T>[] {
  const out: Marker<T>[] = [];
  for (const item of items) {
    const t = ms(ts(item));
    if (t !== undefined && t >= r.from && t <= r.to) out.push({ t, item });
  }
  return out.sort((a, b) => a.t - b.t);
}

// A cluster is one drawn marker: a single row, or a count of rows too close
// together to tell apart, drawn at their middle.
export type Cluster<T> = { x: number; from: number; to: number; items: T[] };

// clusterMarkers groups sorted markers whose x lies within gapPx of the first
// in the group, so a burst of log lines reads as "12" rather than a smear.
export function clusterMarkers<T>(markers: readonly Marker<T>[], r: TimeWindow, f: LaneFrame, gapPx: number): Cluster<T>[] {
  const out: Cluster<T>[] = [];
  let head: { x0: number; from: number; to: number; items: T[] } | undefined;
  const flush = () => {
    if (head) out.push({ x: xOf((head.from + head.to) / 2, r, f), from: head.from, to: head.to, items: head.items });
  };
  for (const m of markers) {
    const x = xOf(m.t, r, f);
    if (head && x - head.x0 < gapPx) {
      head.items.push(m.item);
      head.to = m.t;
      continue;
    }
    flush();
    head = { x0: x, from: m.t, to: m.t, items: [m.item] };
  }
  flush();
  return out;
}

// metricLine is a metric's bucketed series as a lane's line: the chart
// primitive's geometry over the range, at the lane's height.
export function metricLine(
  buckets: readonly ChartBucket[],
  r: TimeWindow,
  f: LaneFrame & { height: number; padY: number },
  resolutionMs: number,
): ChartLayout {
  return bucketLayout([...buckets], { ...f, now: r.to, windowMs: Math.max(r.to - r.from, 1), resolutionMs });
}

// ruleInputs is what the Metrics and Properties lanes show before the operator
// picks: the signal each rule that raised an alarm in range watches, so the
// line that crossed the threshold sits under the span it raised.
export function ruleInputs(
  spans: readonly AlarmSpan[],
  rules: readonly { id: string; name: string; sample_lane: "metric" | "property"; sample_key: string }[],
): { metrics: string[]; properties: string[] } {
  const metrics = new Set<string>();
  const properties = new Set<string>();
  for (const s of spans) {
    const r = rules.find((x) => (s.ruleId ? x.id === s.ruleId : x.name === s.rule));
    if (!r || !r.sample_key) continue;
    (r.sample_lane === "metric" ? metrics : properties).add(r.sample_key);
  }
  return { metrics: [...metrics], properties: [...properties] };
}

// changedPropertyKeys names the properties with a change inside the range, in
// key order: the Properties lane's default.
export function changedPropertyKeys(series: readonly { key: string; points?: readonly { ts: string }[] | null }[], r: TimeWindow): string[] {
  const keys = new Set<string>();
  for (const s of series) {
    if ((s.points ?? []).some((p) => {
      const t = ms(p.ts);
      return t !== undefined && t > r.from && t <= r.to;
    })) keys.add(s.key);
  }
  return [...keys].sort();
}

// cursorStep moves the keyboard's cursor a hundredth of the range, clamped to
// it; with none set it starts at the range's end, the newest instant.
export function cursorStep(t: number | undefined, r: TimeWindow, dir: -1 | 1): number {
  if (t === undefined) return r.to;
  return Math.min(Math.max(t + dir * ((r.to - r.from) / 100), r.from), r.to);
}
