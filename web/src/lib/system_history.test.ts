import { describe, expect, it } from "vitest";
import { incidentReasons, incidentRows, incidentsOf, markerX, uptimePct } from "./system_history";
import type { Alarm } from "./alarms";

// The history tab's pure core (#792): the verdict spans come from the health
// read (timeline.spans, already proven); this file is the CAUSES: every
// member alarm in the window, cleared ones included, merged worst-first-now
// then newest, each placeable on the same axis the spans use.
const now = Date.parse("2026-08-20T17:00:00Z");
const iso = (hoursAgo: number) => new Date(now - hoursAgo * 3600_000).toISOString();

const alarms = (component: string, componentId: string, list: Partial<Alarm>[]): { component: string; componentId: string; alarms: Alarm[] } => ({
  component,
  componentId,
  alarms: list as Alarm[],
});

describe("incidentRows", () => {
  const rows = () =>
    incidentRows(
      [
        alarms("videobar-1", "c-bar", [
          { id: "a1", severity: "critical", message: "No route to host", raised_at: iso(3), active: true },
          { id: "a0", severity: "warning", message: "Fan speed high", raised_at: iso(200), cleared_at: iso(199), active: false },
        ]),
        alarms("display-1", "c-disp", [
          { id: "a2", severity: "warning", message: "Input signal lost", raised_at: iso(60), cleared_at: iso(59), active: false },
        ]),
      ],
      now,
      30 * 24,
    );

  it("merges every member's alarms, active first, then newest, each naming its component", () => {
    const r = rows();
    expect(r.map((x) => x.id)).toEqual(["a1", "a2", "a0"]);
    expect(r[0]).toMatchObject({ component: "videobar-1", componentId: "c-bar", ongoing: true });
    expect(r[1].ongoing).toBe(false);
  });

  it("keeps an alarm raised before the window when it was still active inside it", () => {
    const r = incidentRows(
      [alarms("x", "cx", [{ id: "old", severity: "critical", message: "m", raised_at: iso(31 * 24), cleared_at: iso(29 * 24), active: false }])],
      now,
      30 * 24,
    );
    expect(r).toHaveLength(1);
    const gone = incidentRows(
      [alarms("x", "cx", [{ id: "gone", severity: "critical", message: "m", raised_at: iso(40 * 24), cleared_at: iso(35 * 24), active: false }])],
      now,
      30 * 24,
    );
    expect(gone).toHaveLength(0);
  });
});

describe("markerX", () => {
  it("places a raise on the window axis as a fraction, clamped at the left edge", () => {
    const windowMs = 30 * 24 * 3600_000;
    expect(markerX(iso(15 * 24), now, windowMs)).toBeCloseTo(0.5, 5);
    expect(markerX(iso(40 * 24), now, windowMs)).toBe(0);
  });
});

// The statuspage read (#795 refinement): the window's uptime as the health
// KPI, and the transitions grouped into INCIDENTS, each a contiguous
// unhealthy stretch with the alarms that explain it.
import type { Span } from "./timeline";
const span = (value: string, fromH: number, toH: number): Span<string> =>
  ({ value, from: now - fromH * 3600_000, to: now - toH * 3600_000, weight: (fromH - toH) / (30 * 24) }) as Span<string>;

describe("uptimePct", () => {
  it("is the healthy share of the window, one decimal, statuspage style", () => {
    const spans = [span("healthy", 720, 72), span("degraded", 72, 0)];
    expect(uptimePct(spans)).toBe("90.0");
    expect(uptimePct([span("healthy", 720, 0)])).toBe("100");
    expect(uptimePct([])).toBeNull();
  });
});

describe("incidentsOf", () => {
  it("groups consecutive unhealthy spans into one incident carrying the worst verdict and its sub-spans", () => {
    const spans = [
      span("healthy", 720, 200),
      span("degraded", 200, 190),
      span("outage", 190, 180),
      span("healthy", 180, 72),
      span("incomplete", 72, 0),
    ];
    const inc = incidentsOf(spans, now);
    expect(inc).toHaveLength(2);
    // Ongoing first, then newest.
    expect(inc[0].ongoing).toBe(true);
    expect(inc[0].worst).toBe("incomplete");
    expect(inc[1].worst).toBe("outage");
    expect(inc[1].spans.map((s) => s.value)).toEqual(["degraded", "outage"]);
    expect(inc[1].ongoing).toBe(false);
    expect(inc[1].to - inc[1].from).toBe(20 * 3600_000);
  });

  it("a fully healthy window has no incidents", () => {
    expect(incidentsOf([span("healthy", 720, 0)], now)).toHaveLength(0);
  });
});

describe("incidentReasons", () => {
  const rows = incidentRows(
    [
      alarms("videobar-1", "c-bar", [
        { id: "a1", severity: "critical", message: "No route to host", raised_at: iso(195), cleared_at: iso(185), active: false },
        { id: "a2", severity: "warning", message: "Unrelated, later", raised_at: iso(10), active: true },
      ]),
    ],
    now,
    30 * 24,
  );

  it("keeps only the alarms overlapping the incident's stretch", () => {
    const spans = [span("healthy", 720, 200), span("degraded", 200, 180), span("healthy", 180, 0)];
    const inc = incidentsOf(spans, now)[0];
    const reasons = incidentReasons(inc, rows);
    expect(reasons.map((r) => r.id)).toEqual(["a1"]);
  });

  it("an ongoing alarm overlaps an ongoing incident", () => {
    const spans = [span("healthy", 720, 12), span("degraded", 12, 0)];
    const inc = incidentsOf(spans, now)[0];
    expect(incidentReasons(inc, rows).map((r) => r.id)).toEqual(["a2"]);
  });
});
