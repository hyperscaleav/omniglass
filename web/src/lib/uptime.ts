// Uptime over the entity's RECORDED LIFE (#2435). The healthy share divides by
// the time the owner has existed inside the page range, never the whole range:
// a system watched for four minutes is judged on those four minutes, and the
// label names the window so "100%" is never read as thirty days of evidence.
// Pure, so the system, component and location surfaces (and the overlay
// timeline) share one answer. The range is the page's (#2741): a preset slides
// to `now`, a custom range is measured to its own end, and the label names it.
import { fmtTime } from "./format";
import { HISTORY_DEFAULT_RANGE, rangePhrase, resolveRange, type TimeRange } from "./time_range";

export const RANGE_30D_MS = 30 * 24 * 3600_000;

export type UptimeInput = {
  // The recorded edges inside the range, oldest first.
  transitions: { ts: string; health: string }[];
  // The current health (fills the window when no edge falls inside it).
  health?: string | null;
  // The health in force when the range opened (the read's carried_in).
  carriedIn?: string | null;
  // When the owner's health was first recorded (the read's first_recorded_at).
  firstRecordedAt?: string | null;
  // The page's range; the last 30 days when none is given.
  range?: TimeRange;
  now: number;
};

export type UptimeResult = {
  // One decimal ("99.9"), "100" when never unhealthy, null with nothing to measure.
  pct: string | null;
  // The window actually measured, ms epoch; null when nothing was recorded.
  from: number | null;
  // True when the window is shorter than the range.
  shortened: boolean;
  label: string;
};

const NONE: UptimeResult = { pct: null, from: null, shortened: false, label: "no health recorded yet" };

export function uptimeOver(i: UptimeInput): UptimeResult {
  const range = i.range ?? HISTORY_DEFAULT_RANGE;
  const bounds = resolveRange(range, i.now);
  const rangeStart = bounds.from;
  // A custom range that ended before now is measured to its end.
  const end = Math.min(bounds.to, i.now);
  const edges = [...i.transitions]
    .map((t) => ({ at: Date.parse(t.ts), value: t.health }))
    .filter((e) => e.at <= end)
    .sort((a, b) => a.at - b.at);
  const recorded = i.firstRecordedAt ? Date.parse(i.firstRecordedAt) : edges.length ? edges[0].at : NaN;
  if (Number.isNaN(recorded)) return NONE;
  const from = Math.max(rangeStart, recorded);
  const total = end - from;
  if (total <= 0) return { ...NONE, from };

  // The value in force at the window's left edge: the last edge at or before it,
  // else carried_in (the range opened mid-life), else the first edge's own value.
  const before = edges.filter((e) => e.at <= from);
  const inside = edges.filter((e) => e.at > from);
  const opening = before.length ? before[before.length - 1].value : i.carriedIn ?? inside[0]?.value ?? i.health ?? null;
  if (opening === null) return { ...NONE, from };

  let healthy = 0;
  let cursor = from;
  let value = opening;
  for (const e of inside) {
    if (value === "healthy") healthy += e.at - cursor;
    cursor = e.at;
    value = e.value;
  }
  if (value === "healthy") healthy += end - cursor;

  const p = (healthy / total) * 100;
  const shortened = from > rangeStart;
  return {
    pct: p >= 100 ? "100" : p.toFixed(1),
    from,
    shortened,
    label: labelOf(range, from, end, shortened),
  };
}

// The window in words: a preset by its span ("of the last hour"), or since the
// first record when the owner is younger than it; a custom range by its dates.
function labelOf(range: TimeRange, from: number, end: number, shortened: boolean): string {
  const at = (ms: number) => fmtTime(new Date(ms).toISOString());
  if (range.kind === "absolute") return `${at(from)} → ${at(end)}`;
  return shortened ? `since ${at(from)}` : `of ${rangePhrase(range)}`;
}
