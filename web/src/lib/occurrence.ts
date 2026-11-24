import type { ComponentEvent } from "./events";
import type { ComponentLog, LogSeverity } from "./logs";
import { severityVariant } from "./logs";
import type { SystemEvent } from "./system_activity";
import type { SystemLog } from "./system_activity";

// The occurrence: one event or one log line as a list row sees it (#2437). The
// two lanes keep their own wire shapes, but a row reads, filters and opens the
// same way wherever it is listed, so each is normalized here once and
// OccurrenceRow draws only this.

export type OccurrenceKind = "event" | "log";

export type Occurrence = {
  kind: OccurrenceKind;
  // A key the list can hold an open row by; ts and the message are not unique.
  id: string;
  ts: string;
  // The kind badge: an event's key, or a log's severity colored by level.
  badge: string;
  badgeClass: string;
  // The owning entity when the list spans several (a system's members).
  owner?: string;
  message: string;
  source?: string;
  instance?: string;
  provenance?: string;
  origin?: string;
  facility?: string;
  correlationId?: string;
  receivedAt?: string;
  attributes?: unknown;
  labels?: unknown;
  rule?: { name: string; id?: string };
  // The alarm a rule's event opened or resolved, and the component whose
  // timeline it lives on.
  alarm?: { id: string; component: string };
};

// The filter every occurrence list shares: a log severity threshold, an event
// key, a source, and free text. Each narrows the server read.
export type OccurrenceFilter = { severity?: LogSeverity | ""; key?: string; source?: string; q?: string };

export const EMPTY_FILTER: OccurrenceFilter = {};

export const isFiltered = (f: OccurrenceFilter) => Boolean(f.severity || f.key || f.source || f.q);

export function fromEvent(e: ComponentEvent | SystemEvent, i: number, component?: string): Occurrence {
  const sys = e as Partial<SystemEvent>;
  const alarmComponent = sys.owner_kind ? (sys.owner_kind === "component" ? sys.owner_id : undefined) : component;
  return {
    kind: "event",
    id: `e:${i}:${e.ts}:${e.key}`,
    ts: e.ts,
    badge: e.key,
    badgeClass: "badge-ghost",
    owner: sys.owner,
    message: e.message,
    source: e.source,
    instance: e.instance,
    provenance: e.provenance,
    origin: e.origin,
    attributes: e.attributes,
    rule: e.derived_by_rule ? { name: e.derived_by_rule, id: e.derived_by_rule_id } : undefined,
    alarm: e.alarm_id && alarmComponent ? { id: e.alarm_id, component: alarmComponent } : undefined,
  };
}

export function fromLog(l: ComponentLog | SystemLog, i: number): Occurrence {
  const sys = l as Partial<SystemLog>;
  return {
    kind: "log",
    id: `l:${i}:${l.ts}`,
    ts: l.ts,
    badge: l.severity || "log",
    badgeClass: severityVariant(l.severity),
    owner: sys.component,
    message: l.message,
    source: l.source,
    instance: l.instance,
    facility: l.facility,
    correlationId: l.correlation_id,
    receivedAt: l.received_at,
    attributes: l.attributes,
    labels: l.labels,
  };
}

// A line that arrived well after the time it claims says so; within a minute
// the two are the same moment.
export function arrivedApart(o: Pick<Occurrence, "ts" | "receivedAt">): boolean {
  if (!o.receivedAt) return false;
  return Math.abs(Date.parse(o.receivedAt) - Date.parse(o.ts)) > 60_000;
}

export type MessagePart = { text: string } | { ref: string; label: string; short: string };

const HEX64 = /\b[0-9a-f]{64}\b/gi;
// The word naming what an id is: "task 3f..", "task_id=3f..", "task: 3f..".
const NAMING = /([A-Za-z][A-Za-z0-9]*?)(?:[_-]?id)?\s*[:=]?\s*$/;

// messageParts splits a message so a task or other internal id (a 64-hex
// string) renders as a short labelled reference, never raw. The word that names
// the id is folded into the reference's label; with none the label is "id".
// The full id stays reachable (the reference's title, and the open detail).
export function messageParts(message: string): MessagePart[] {
  const parts: MessagePart[] = [];
  let last = 0;
  for (const m of message.matchAll(HEX64)) {
    const start = m.index ?? 0;
    let before = message.slice(last, start);
    let label = "id";
    const naming = NAMING.exec(before);
    if (naming && naming[1]) {
      label = naming[1].toLowerCase();
      before = before.slice(0, naming.index);
    }
    if (before) parts.push({ text: before });
    parts.push({ ref: m[0], label, short: m[0].slice(0, 8) });
    last = start + m[0].length;
  }
  if (last < message.length) parts.push({ text: message.slice(last) });
  return parts;
}
