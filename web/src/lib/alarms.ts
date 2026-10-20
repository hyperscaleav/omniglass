import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { compareSeverity, type SeverityLevel } from "./severity";

// The alarms data layer: thin typed wrappers over the generated client, plus the
// pure ordering and splitting the panel renders.
//
// An ALARM is a condition recorded on a COMPONENT, and it impairs that component's
// own verdict wholesale (#626): any active alarm degrades it, one at an outage
// severity level (high and above, see lib/severity) is an outage. A system role the component fills no longer counts it toward quorum
// while it is down, so the role can drop below quorum and its impact becomes the
// system's verdict. That is the whole chain: alarm on a component, component down,
// role below quorum, verdict.
//
// Clearing keeps the row: what was wrong and when it was wrong outlives the fix,
// which is the point of the recorded history.

export type Alarm = components["schemas"]["AlarmBody"];

// Both the active set and the recent history come from one route, so one cache
// key covers both and clearing an alarm invalidates a single entry.
// The prefix every component's alarms share, for a write that moves them all.
export const COMPONENT_ALARMS_PREFIX = ["component-alarms"] as const;
export const componentAlarmsKey = (name: string) => [...COMPONENT_ALARMS_PREFIX, name] as const;

// The alarms read with the component's own verdict beside them (#934), the
// one its active alarms make. Keyed under componentAlarmsKey, so whatever
// invalidates a component's alarms (an acknowledge) refreshes this too.
export const componentAlarmsReadKey = (name: string) => [...componentAlarmsKey(name), "verdict"] as const;
export type ComponentAlarmsRead = { alarms: Alarm[]; verdict?: string };

export async function componentAlarmsRead(name: string): Promise<ComponentAlarmsRead> {
  const { data, error } = await api.GET("/components/{name}/alarms", {
    params: { path: { name }, query: { include_cleared: true } },
  });
  if (error) throw error;
  return { alarms: (data?.alarms ?? []) as Alarm[], verdict: data?.verdict };
}

// componentAlarms reads every alarm on the component, cleared ones included: the
// panel shows what is wrong now above what was recently wrong, and the cleared row
// is how an operator confirms a fix landed.
export async function componentAlarms(name: string): Promise<Alarm[]> {
  const { data, error } = await api.GET("/components/{name}/alarms", {
    params: { path: { name }, query: { include_cleared: true } },
  });
  if (error) throw error;
  return (data?.alarms ?? []) as Alarm[];
}

export type RaiseAlarm = {
  // A severity level by name, from the registry (GET /severity-levels).
  severity: string;
  message?: string;
  dedup_key?: string;
};

export async function raiseAlarm(name: string, body: RaiseAlarm): Promise<Alarm> {
  const { data, error } = await api.POST("/components/{name}/alarms", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Alarm;
}

export async function clearAlarm(name: string, id: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}/alarms/{id}", { params: { path: { name, id } } });
  if (error) throw error;
}

// acknowledgeAlarm records that a human has seen this alarm. It is ORTHOGONAL to
// clearing: the alarm stays exactly as raised as it was, no health is recomputed,
// and the component is exactly as broken afterwards. Acknowledging is not fixing.
export async function acknowledgeAlarm(name: string, id: string): Promise<Alarm> {
  const { data, error } = await api.POST("/components/{name}/alarms/{id}:acknowledge", {
    params: { path: { name, id } },
  });
  if (error) throw error;
  return data as Alarm;
}

// unacknowledgedCount is the queue an operator actually works: raised, and nobody
// has looked at it yet. Cleared alarms are excluded because the queue is about
// what still needs somebody, not about the history.
export function unacknowledgedCount(list: Alarm[]): number {
  return list.filter((a) => a.active && !a.acknowledged).length;
}

// sortAlarms puts the worst first (by the registry's rank, unknown names last),
// and the most recent first within a severity, so the line that explains the
// outage is the line the eye lands on. The levels are passed in so the sort stays
// pure; without them every alarm ties on severity and the order is by recency.
export function sortAlarms<T extends { severity: string; raised_at: string }>(list: T[], levels?: SeverityLevel[] | null): T[] {
  const bySeverity = compareSeverity(levels);
  return [...list].sort((a, b) => bySeverity(a.severity, b.severity) || b.raised_at.localeCompare(a.raised_at));
}

// splitAlarms keeps what is wrong now apart from what was wrong: an active alarm is
// one the server has not cleared. Both halves are ordered worst first.
export function splitAlarms(list: Alarm[], levels?: SeverityLevel[] | null): { active: Alarm[]; cleared: Alarm[] } {
  return {
    active: sortAlarms(list.filter((a) => a.active), levels),
    cleared: sortAlarms(list.filter((a) => !a.active), levels),
  };
}
