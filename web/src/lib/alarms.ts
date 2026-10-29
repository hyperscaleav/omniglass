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

// The system's end (#940): every member's alarms in one read, active ones
// whatever their age and those cleared in the last 30 days, each naming its
// component. A system's Activity polls it, one request however many members
// the system has, rather than one alarms read per member.
export const systemAlarmsKey = (name: string) => ["system-alarms", name] as const;

export async function systemAlarms(name: string): Promise<Alarm[]> {
  const { data, error } = await api.GET("/systems/{name}/alarms", { params: { path: { name } } });
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
// The snooze (#1318): a bounded silence on the alarm's condition (component
// and dedup key), not on one row. The console offers four presets inside the
// server's bound of 1m to 7d.
export const SNOOZE_PRESETS: { duration: string; label: string }[] = [
  { duration: "15m", label: "15 minutes" },
  { duration: "1h", label: "1 hour" },
  { duration: "4h", label: "4 hours" },
  { duration: "24h", label: "24 hours" },
];

export async function snoozeAlarm(name: string, id: string, duration: string): Promise<Alarm> {
  const { data, error } = await api.POST("/components/{name}/alarms/{id}:snooze", {
    params: { path: { name, id } },
    body: { duration },
  });
  if (error) throw error;
  return data as Alarm;
}

export async function unsnoozeAlarm(name: string, id: string): Promise<Alarm> {
  const { data, error } = await api.POST("/components/{name}/alarms/{id}:unsnooze", {
    params: { path: { name, id } },
  });
  if (error) throw error;
  return data as Alarm;
}

// snoozedUntilWords words a snooze's deadline as the badge reads it: the
// time alone when it ends today, the day and time when it ends later.
export function snoozedUntilWords(until: string, now: number = Date.now()): string {
  const d = new Date(until);
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (d.toDateString() === new Date(now).toDateString()) return `snoozed until ${time}`;
  return `snoozed until ${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`;
}

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

// ---- an alarm's timeline (#950) ---------------------------------------------

export type AlarmTimelineEntry = components["schemas"]["AlarmTimelineEntryBody"];

// Every alarm's timeline shares one prefix, so an acknowledge or a clear can
// refresh whichever timeline is open.
export const ALARM_TIMELINE_PREFIX = ["alarm-timeline"] as const;
export const alarmTimelineKey = (component: string, id: string) => [...ALARM_TIMELINE_PREFIX, component, id] as const;
// An open timeline refreshes on its alarm list's cadence.
export const ALARM_TIMELINE_POLL_MS = 5_000;

// ageWords says how long ago, to the second, against one clock (now): "14s
// ago", "2m 14s ago", "2h 3m ago", "3d 4h ago". Seconds stay readable for the
// lines of one incident, which often land seconds apart.
export function ageWords(at: string, now: number): string {
  const s = Math.max(0, Math.floor((now - Date.parse(at)) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m ago`;
  return `${Math.floor(h / 24)}d ${h % 24}h ago`;
}

// alarmTimeline reads one alarm's history, oldest first: the events that carry
// it (a rule's fire and clear events) merged by time with its audit rows (the
// raise, each acknowledgement, the clear).
export async function alarmTimeline(component: string, id: string): Promise<AlarmTimelineEntry[]> {
  const { data, error } = await api.GET("/components/{name}/alarms/{id}:timeline", {
    params: { path: { name: component, id } },
  });
  if (error) throw error;
  return (data?.entries ?? []) as AlarmTimelineEntry[];
}

const AUDIT_WORDS: Record<string, string> = {
  raise: "raised",
  clear: "cleared",
  acknowledge: "acknowledged",
  snooze: "snoozed",
  unsnooze: "unsnoozed",
  "snooze-end": "snooze ended",
};

// timelineWhat is an event or audit entry's verb as an operator reads it: an
// event by its type (condition-started), an audit row by what was done to the
// alarm. A notification is not a sentence (#1413): it reads as a status row,
// its status in the shared NotificationStatusPill.
export function timelineWhat(e: Pick<AlarmTimelineEntry, "kind" | "what">): string {
  return e.kind === "audit" ? (AUDIT_WORDS[e.what] ?? e.what) : e.what;
}

// timelineWho says who, on an event or audit entry: the person who acted,
// else the rule the entry belongs to (the rule wrote the event, or the
// platform acted for it), else the platform itself.
export function timelineWho(e: Pick<AlarmTimelineEntry, "actor" | "rule">): string {
  if (e.actor) return e.actor;
  if (e.rule) return `rule ${e.rule}`;
  return "the platform";
}

// retryNotification re-queues a failed notification for a fresh run of
// attempts (#618): the same message, the same Idempotency-Key.
export async function retryNotification(id: string): Promise<void> {
  const { error } = await api.POST("/notifications/{id}:retry", { params: { path: { id } } });
  if (error) throw error;
}
