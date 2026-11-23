import { describe, expect, it } from "vitest";
import { uuidFor } from "./testids";
import {
  alarmSpans,
  alarmTip,
  clusterMarkers,
  healthSpans,
  markersInRange,
  changedPropertyKeys,
  cursorStep,
  metricLine,
  propertyBands,
  ruleInputs,
  spanAt,
  xOf,
} from "./timeline_lanes";

const T0 = Date.parse("2026-11-23T10:00:00Z");
const MIN = 60_000;
const at = (m: number) => new Date(T0 + m * MIN).toISOString();
const range = { from: T0, to: T0 + 60 * MIN };
const now = T0 + 60 * MIN;

describe("healthSpans", () => {
  it("holds a carried-in verdict across the whole range when nothing changed", () => {
    expect(healthSpans([], range, { carriedIn: "healthy", now })).toEqual([{ value: "healthy", from: range.from, to: range.to }]);
  });

  it("opens with the carried-in verdict and changes at each edge", () => {
    const s = healthSpans(
      [
        { ts: at(10), health: "degraded" },
        { ts: at(25), health: "healthy" },
      ],
      range,
      { carriedIn: "healthy", now },
    );
    expect(s).toEqual([
      { value: "healthy", from: T0, to: T0 + 10 * MIN },
      { value: "degraded", from: T0 + 10 * MIN, to: T0 + 25 * MIN },
      { value: "healthy", from: T0 + 25 * MIN, to: range.to },
    ]);
  });

  it("takes the last edge before the range as the verdict in force at its start", () => {
    const s = healthSpans([{ ts: at(-30), health: "outage" }, { ts: at(30), health: "healthy" }], range, { now });
    expect(s[0]).toEqual({ value: "outage", from: T0, to: T0 + 30 * MIN });
  });

  it("leaves the stretch before the first edge unknown when nothing says what it was", () => {
    const s = healthSpans([{ ts: at(20), health: "degraded" }], range, { now });
    expect(s).toEqual([{ value: "degraded", from: T0 + 20 * MIN, to: range.to }]);
  });

  it("fills a range reaching now with the current verdict when nothing else is known", () => {
    expect(healthSpans([], range, { current: "healthy", now })).toEqual([{ value: "healthy", from: range.from, to: range.to }]);
  });

  it("does not paint a past range with today's verdict", () => {
    const past = { from: T0 - 120 * MIN, to: T0 - 60 * MIN };
    expect(healthSpans([], past, { current: "healthy", now })).toEqual([]);
  });

  it("stops at now inside a range that runs past it, and drops edges after the range", () => {
    const s = healthSpans([{ ts: at(70), health: "outage" }], { from: T0, to: T0 + 120 * MIN }, { carriedIn: "healthy", now: T0 + 50 * MIN });
    expect(s).toEqual([{ value: "healthy", from: T0, to: T0 + 50 * MIN }]);
  });

  it("merges repeated edges of one verdict", () => {
    const s = healthSpans([{ ts: at(10), health: "healthy" }], range, { carriedIn: "healthy", now });
    expect(s).toEqual([{ value: "healthy", from: T0, to: range.to }]);
  });
});

const alarm = (o: Partial<{ id: string; raised_at: string; cleared_at: string; severity: string; acknowledged: boolean }>) => ({
  id: o.id ?? "a1",
  component: "display-1",
  component_id: "c1",
  severity: o.severity ?? "high",
  message: "Reachability lost",
  raised_by_rule: "reachability-lost",
  raised_at: o.raised_at ?? at(10),
  cleared_at: o.cleared_at,
  acknowledged: o.acknowledged ?? false,
  active: o.cleared_at === undefined,
});

describe("alarmSpans", () => {
  it("draws a raised-then-cleared alarm from raise to clear, with its duration", () => {
    const [s] = alarmSpans([alarm({ raised_at: at(10), cleared_at: at(24) })], range, now);
    expect(s).toMatchObject({ id: "a1", from: T0 + 10 * MIN, to: T0 + 24 * MIN, duration: 14 * MIN, open: false, clippedStart: false, rule: "reachability-lost" });
  });

  it("clips an alarm open at the range's start but keeps its whole duration", () => {
    const [s] = alarmSpans([alarm({ raised_at: at(-30), cleared_at: at(5) })], range, now);
    expect(s).toMatchObject({ from: T0, to: T0 + 5 * MIN, clippedStart: true, duration: 35 * MIN });
  });

  it("runs an alarm still open at the range's end to the end, open", () => {
    const [s] = alarmSpans([alarm({ raised_at: at(50) })], range, now);
    expect(s).toMatchObject({ from: T0 + 50 * MIN, to: range.to, open: true, duration: 10 * MIN });
  });

  it("gives overlapping alarms a row each, oldest first", () => {
    const s = alarmSpans(
      [alarm({ id: "b", raised_at: at(20), cleared_at: at(40) }), alarm({ id: "a", raised_at: at(10), cleared_at: at(30) })],
      range,
      now,
    );
    expect(s.map((x) => [x.id, x.row])).toEqual([
      ["a", 0],
      ["b", 1],
    ]);
  });

  it("drops alarms wholly outside the range", () => {
    const s = alarmSpans([alarm({ id: "old", raised_at: at(-50), cleared_at: at(-40) }), alarm({ id: "later", raised_at: at(70) })], range, now);
    expect(s).toEqual([]);
  });

  it("names raise, clear, duration and acknowledgement in its tip", () => {
    const [s] = alarmSpans([alarm({ raised_at: at(10), cleared_at: at(24), acknowledged: true })], range, now);
    const tip = alarmTip(s, (iso) => iso.slice(11, 16));
    expect(tip).toContain("raised 10:10");
    expect(tip).toContain("cleared 10:24");
    expect(tip).toContain("raised for 14m");
    expect(tip).toContain("acknowledged");
    expect(tip).toContain("reachability-lost");
  });

  it("says an open alarm is still raised", () => {
    const [s] = alarmSpans([alarm({ raised_at: at(50) })], range, now);
    expect(alarmTip(s, (iso) => iso.slice(11, 16))).toContain("still raised, 10m so far");
  });
});

describe("propertyBands", () => {
  it("holds each value until the next change, from the carried-in value", () => {
    const b = propertyBands([{ ts: at(15), value: "off" }, { ts: at(40), value: "on" }], "on", range, now);
    expect(b).toEqual([
      { value: "on", from: T0, to: T0 + 15 * MIN },
      { value: "off", from: T0 + 15 * MIN, to: T0 + 40 * MIN },
      { value: "on", from: T0 + 40 * MIN, to: range.to },
    ]);
  });

  it("merges a repeated value and drops changes outside the range", () => {
    const b = propertyBands([{ ts: at(-5), value: "on" }, { ts: at(20), value: "on" }, { ts: at(80), value: "off" }], undefined, range, now);
    expect(b).toEqual([{ value: "on", from: T0, to: range.to }]);
  });

  it("is empty when nothing is known", () => {
    expect(propertyBands([], undefined, range, now)).toEqual([]);
  });
});

describe("markers", () => {
  const rows = [{ ts: at(-1) }, { ts: at(5) }, { ts: at(6) }, { ts: at(61) }, { ts: at(30) }];
  it("keeps only the rows inside the range, oldest first", () => {
    expect(markersInRange(rows, (r) => r.ts, range).map((m) => m.t)).toEqual([T0 + 5 * MIN, T0 + 6 * MIN, T0 + 30 * MIN]);
  });

  it("clusters markers closer than the gap into a count, and leaves far ones alone", () => {
    const frame = { width: 600, padLeft: 0, padRight: 0 };
    const ms = markersInRange(rows, (r) => r.ts, range);
    const c = clusterMarkers(ms, range, frame, 12);
    expect(c.map((x) => x.items.length)).toEqual([2, 1]);
    expect(c[0].from).toBe(T0 + 5 * MIN);
    expect(c[0].to).toBe(T0 + 6 * MIN);
    expect(c[0].x).toBeCloseTo(xOf(T0 + 5.5 * MIN, range, frame), 5);
  });
});

describe("geometry and the cursor", () => {
  it("places an instant on the plot between the pads", () => {
    const frame = { width: 120, padLeft: 10, padRight: 10 };
    expect(xOf(range.from, range, frame)).toBe(10);
    expect(xOf(range.to, range, frame)).toBe(110);
    expect(xOf(T0 + 30 * MIN, range, frame)).toBe(60);
  });

  it("reads the span in force at an instant, or none", () => {
    const s = healthSpans([{ ts: at(10), health: "degraded" }], range, { carriedIn: "healthy", now });
    expect(spanAt(s, T0 + 12 * MIN)?.value).toBe("degraded");
    expect(spanAt(s, T0 - MIN)).toBeUndefined();
  });

  it("draws a metric as a line over the range", () => {
    const l = metricLine(
      [
        { ts: at(0), count: 1, min: 1, max: 1, avg: 1, last: 1 },
        { ts: at(30), count: 1, min: 3, max: 3, avg: 3, last: 3 },
      ],
      range,
      { width: 600, height: 40, padLeft: 0, padRight: 0, padY: 4 },
      30 * MIN,
    );
    expect(l.linePath).not.toBe("");
    expect(l.points).toHaveLength(2);
  });
});

describe("lane defaults", () => {
  it("defaults the metrics and properties to the inputs of the rules that raised alarms in range", () => {
    const spans = alarmSpans([alarm({ raised_at: at(10), cleared_at: at(20) })], range, now);
    const rules = [
      { id: uuidFor("tl-rule-1"), name: "reachability-lost", sample_lane: "property" as const, sample_key: "reachable" },
      { id: uuidFor("tl-rule-2"), name: "too-hot", sample_lane: "metric" as const, sample_key: "temperature" },
    ];
    expect(ruleInputs(spans, rules)).toEqual({ metrics: [], properties: ["reachable"] });
    const hot = alarmSpans([{ ...alarm({ raised_at: at(10) }), raised_by_rule: "too-hot", raised_by_rule_id: uuidFor("tl-rule-2") }], range, now);
    expect(ruleInputs(hot, rules)).toEqual({ metrics: ["temperature"], properties: [] });
  });

  it("names the properties that changed inside the range", () => {
    const series = [
      { key: "power", instance: "", carried_in: "on", points: [{ ts: at(15), value: "off" }] },
      { key: "input", instance: "", carried_in: "hdmi1", points: [] },
      { key: "volume", instance: "", points: [{ ts: at(-10), value: "3" }] },
    ];
    expect(changedPropertyKeys(series, range)).toEqual(["power"]);
  });

  it("steps the cursor a hundredth of the range, starting from the end", () => {
    expect(cursorStep(undefined, range, -1)).toBe(range.to);
    expect(cursorStep(range.to, range, -1)).toBe(range.to - 36_000);
    expect(cursorStep(range.from, range, -1)).toBe(range.from);
  });
});
