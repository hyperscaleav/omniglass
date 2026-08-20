// The history tab's pure core (#792): the statuspage read. The verdict spans
// over the window come from the health read through timeline.spans (already
// proven); this file derives the CAUSES beside them: every member's alarms,
// cleared ones included, merged onto one list and one axis. Ongoing first
// (they are what is wrong now), then newest raise first; an alarm that
// cleared before the window began is history the window does not show.
import { severityRank, type Alarm } from "./alarms";

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
