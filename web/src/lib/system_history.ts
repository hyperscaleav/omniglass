// The history tab's pure core (#792): the statuspage read. The verdict spans
// over the window come from the health read through timeline.spans (already
// proven); this file derives the CAUSES beside them: every member's alarms,
// cleared ones included, merged onto one list and one axis. Ongoing first
// (they are what is wrong now), then newest raise first; an alarm that
// cleared before the window began is history the window does not show.
import { severityRank, type Alarm } from "./alarms";
import { verdictRank } from "./health";
import type { Span } from "./timeline";

export type MemberAlarms = { component: string; componentId: string; alarms: Alarm[] };

export type IncidentRow = {
  id: string;
  severity: string;
  message: string;
  component: string;
  componentId: string;
  raisedAt: string;
  clearedAt?: string;
  ongoing: boolean;
};

export function incidentRows(members: MemberAlarms[], now: number, windowHours: number): IncidentRow[] {
  const start = now - windowHours * 3600_000;
  const rows: IncidentRow[] = [];
  for (const m of members) {
    for (const a of m.alarms ?? []) {
      const clearedAt = (a as { cleared_at?: string }).cleared_at;
      const activeInWindow = a.active || (clearedAt ? Date.parse(clearedAt) >= start : Date.parse(a.raised_at) >= start);
      if (!activeInWindow) continue;
      rows.push({
        id: a.id,
        severity: a.severity,
        message: a.message,
        component: m.component,
        componentId: m.componentId,
        raisedAt: a.raised_at,
        clearedAt: clearedAt ?? undefined,
        ongoing: a.active,
      });
    }
  }
  return rows.sort(
    (x, y) =>
      Number(y.ongoing) - Number(x.ongoing) ||
      severityRank(x.severity) - severityRank(y.severity) ||
      y.raisedAt.localeCompare(x.raisedAt),
  );
}

// markerX places a moment on the window's axis as a fraction of its width,
// clamped to the left edge for anything older than the window: the marker
// says "this raise sits here under the strip".
export function markerX(iso: string, now: number, windowMs: number): number {
  const f = 1 - (now - Date.parse(iso)) / windowMs;
  return Math.min(Math.max(f, 0), 1);
}

// ---------------------------------------------------------------------------
// The statuspage read (#795 refinement): the health KPI over the window and
// the transitions grouped into INCIDENTS, the way statuspage.io or OneUptime
// tell the story: uptime up top, then each unhealthy stretch as an entry
// whose reasoning expands.

// uptimePct is the healthy share of the recorded window, one decimal ("99.9"),
// the clean "100" when nothing was ever unhealthy, null with no record.
export function uptimePct(spans: Span<string>[]): string | null {
  if (spans.length === 0) return null;
  const total = spans.reduce((a, s) => a + (s.to - s.from), 0);
  if (total <= 0) return null;
  const healthy = spans.filter((s) => s.value === "healthy").reduce((a, s) => a + (s.to - s.from), 0);
  const pct = (healthy / total) * 100;
  return pct >= 100 ? "100" : pct.toFixed(1);
}

export type Incident = {
  from: number;
  to: number;
  ongoing: boolean;
  // The worst verdict the stretch reached: what the entry wears.
  worst: string;
  // The sub-transitions inside the stretch, oldest first.
  spans: Span<string>[];
};

// incidentsOf groups consecutive unhealthy spans into incidents, ongoing
// first and then newest first: one entry per contiguous stretch away from
// healthy, however many verdict changes it contained.
export function incidentsOf(spans: Span<string>[], now: number): Incident[] {
  const out: Incident[] = [];
  let cur: Span<string>[] = [];
  const flush = () => {
    if (cur.length === 0) return;
    const from = cur[0].from;
    const to = cur[cur.length - 1].to;
    out.push({
      from,
      to,
      ongoing: Math.abs(to - now) < 1000,
      worst: [...cur].sort((a, b) => verdictRank(b.value as never) - verdictRank(a.value as never))[0].value,
      spans: cur,
    });
    cur = [];
  };
  for (const s of spans) {
    if (s.value === "healthy") flush();
    else cur.push(s);
  }
  flush();
  return out.sort((a, b) => Number(b.ongoing) - Number(a.ongoing) || b.from - a.from);
}

// incidentReasons keeps the alarms whose own lifetime overlaps the incident's
// stretch: the expandable "why" under each entry. An alarm with no clear is
// open-ended; an ongoing incident's right edge is now.
export function incidentReasons(inc: Incident, rows: IncidentRow[]): IncidentRow[] {
  return rows.filter((r) => {
    const raised = Date.parse(r.raisedAt);
    const cleared = r.clearedAt ? Date.parse(r.clearedAt) : Infinity;
    return raised <= inc.to && cleared >= inc.from;
  });
}
