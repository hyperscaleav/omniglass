import { useSearchParams } from "@solidjs/router";
import { createMemo, type Accessor } from "solid-js";
import { fmtTime } from "./format";

// The page's one time range (#2304): every time-bound surface on a page reads
// the range the operator picked, and the address carries it, so a copied link
// reproduces what the sender saw.
//
// The API, small on purpose (the charts and timelines build on it):
//
//   TimeRange                       a preset sliding to now, or an absolute [from, to)
//   parseRange(searchParams)        the URL's range; anything unparseable is the default
//   serializeRange(range)           the search-param patch that writes it back
//   resolveRange(range, now)        [from, to] in epoch ms at an injected clock
//   useTimeRange()                  { range, setRange } over the router's search params
//
// and three helpers for reads: rangeHours (a preset's width, for the series
// routes' `hours`), rangeStepSeconds (a bucket width that keeps a chart a few
// hundred buckets wide) and rangeLabel (the range in words).
//
// The address: `?range=1h|6h|24h|7d|30d` for a preset, `?from=<rfc3339>&to=<rfc3339>`
// for an absolute range, nothing for the default (24h), the way the tab rail
// leaves its default facet out. A valid absolute range wins over a `range`
// beside it; a broken one is ignored, and a broken `range` is the default. A
// parse never throws: a hand-edited link lands on a working page.

export type PresetKey = "1h" | "6h" | "24h" | "7d" | "30d";

export type TimeRange =
  | { kind: "preset"; preset: PresetKey }
  // Epoch milliseconds; `to` exclusive and after `from`.
  | { kind: "absolute"; from: number; to: number };

const HOUR = 3600_000;
const DAY = 24 * HOUR;

export const PRESETS: readonly { key: PresetKey; label: string; ms: number }[] = [
  { key: "1h", label: "Last 1 hour", ms: HOUR },
  { key: "6h", label: "Last 6 hours", ms: 6 * HOUR },
  { key: "24h", label: "Last 24 hours", ms: DAY },
  { key: "7d", label: "Last 7 days", ms: 7 * DAY },
  { key: "30d", label: "Last 30 days", ms: 30 * DAY },
];

export const DEFAULT_RANGE = { kind: "preset", preset: "24h" } as const satisfies TimeRange;

const presetOf = (key: PresetKey) => PRESETS.find((p) => p.key === key)!;

type ParamSource = URLSearchParams | Record<string, string | string[] | undefined>;

const first = (src: ParamSource, key: string): string | undefined => {
  if (src instanceof URLSearchParams) return src.get(key) ?? undefined;
  const v = src[key];
  return Array.isArray(v) ? v[0] : v;
};

// RFC 3339 date-time: a full date, a time to the second (fraction optional),
// and a zone. Date.parse alone accepts far looser text than a link should.
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/i;

const instant = (s: string | undefined): number | undefined => {
  if (!s || !RFC3339.test(s)) return undefined;
  const t = Date.parse(s);
  return Number.isNaN(t) ? undefined : t;
};

export function parseRange(src: ParamSource): TimeRange {
  const from = instant(first(src, "from"));
  const to = instant(first(src, "to"));
  if (from !== undefined && to !== undefined && from < to) return { kind: "absolute", from, to };
  const key = first(src, "range");
  if (key && PRESETS.some((p) => p.key === key)) return { kind: "preset", preset: key as PresetKey };
  return DEFAULT_RANGE;
}

// The patch for the router's setSearchParams: every key the range owns, the
// ones it does not use set to undefined so they leave the address.
export function serializeRange(r: TimeRange): { range: string | undefined; from: string | undefined; to: string | undefined } {
  if (r.kind === "absolute") return { range: undefined, from: new Date(r.from).toISOString(), to: new Date(r.to).toISOString() };
  return { range: r.preset === DEFAULT_RANGE.preset ? undefined : r.preset, from: undefined, to: undefined };
}

export function resolveRange(r: TimeRange, now: number): { from: number; to: number } {
  if (r.kind === "absolute") return { from: r.from, to: r.to };
  return { from: now - presetOf(r.preset).ms, to: now };
}

// rangeHours is a preset's width in hours: the `hours` a series route counts
// back from now. An absolute range has none (those routes cannot honor it).
export function rangeHours(r: TimeRange): number | undefined {
  return r.kind === "preset" ? presetOf(r.preset).ms / HOUR : undefined;
}

const STEPS_S = [60, 300, 900, 3600, 3 * 3600, 6 * 3600, 86400, 7 * 86400];
const MAX_BUCKETS = 720;

// rangeStepSeconds is the bucket width for a chart over windowMs: the finest
// step that keeps it at most 720 buckets, in whole hours past two days (the
// series route raises a longer window's step to whole hours anyway).
export function rangeStepSeconds(windowMs: number): number {
  const candidates = windowMs > 2 * DAY ? STEPS_S.filter((s) => s >= 3600) : STEPS_S;
  return candidates.find((s) => windowMs / 1000 / s <= MAX_BUCKETS) ?? candidates[candidates.length - 1];
}

export function rangeLabel(r: TimeRange): string {
  if (r.kind === "preset") return presetOf(r.preset).label;
  return `${fmtTime(new Date(r.from).toISOString())} → ${fmtTime(new Date(r.to).toISOString())}`;
}

// useTimeRange reads and writes the page's range in the router's search
// params. Setting it keeps every other param (the tab, a filter). `from` also
// names the template a row was created from (?from=<template>, #1329), so
// from and to are cleared only when leaving an absolute range, the one case
// they are the range's own.
export function useTimeRange(): { range: Accessor<TimeRange>; setRange: (r: TimeRange) => void } {
  const [params, setParams] = useSearchParams();
  const range = createMemo(() => parseRange(params as Record<string, string | string[] | undefined>), DEFAULT_RANGE, {
    equals: (a, b) => JSON.stringify(a) === JSON.stringify(b),
  });
  return {
    range,
    setRange: (r) => {
      const patch: Record<string, string | undefined> = serializeRange(r);
      if (r.kind === "preset" && range().kind === "preset") {
        delete patch.from;
        delete patch.to;
      }
      setParams(patch);
    },
  };
}
