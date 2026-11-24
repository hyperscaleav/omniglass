import { api } from "../api/client";
import type { EventBody } from "../api/api.gen";

// The events data layer: a thin typed wrapper over the generated client for a
// component's recent events. Where reachability samples a value on a cadence (a
// metric or a state), the event sink collects discrete occurrences a component
// published natively or a rule derived, each a message stamped when it happened. The
// API returns them newest first (last 24 hours, capped), so this layer is pure I/O
// over the generated client and unit-testable against a mocked fetch. Shapes follow
// the OpenAPI (see api/events.go); the row type is the generated EventBody, never
// hand-typed.

export type ComponentEvent = EventBody;
export type ComponentEvents = { component: string; events: ComponentEvent[] };

// EventFilter is the read's narrowing (#2437): the event type's name, the
// transport, and a substring of the message, each empty for all.
export type EventFilter = { key?: string; source?: string; q?: string };

export const EVENTS_KEY = (name: string, f: EventFilter = {}) => ["events", name, f.key ?? "", f.source ?? "", f.q ?? ""] as const;

// eventQuery renders a filter as the read's query parameters, leaving out what is empty.
export function eventQuery(f: EventFilter = {}): EventFilter {
  const q: EventFilter = {};
  if (f.key) q.key = f.key;
  if (f.source) q.source = f.source;
  if (f.q) q.q = f.q;
  return q;
}

export async function getEvents(name: string, f: EventFilter = {}): Promise<ComponentEvents> {
  const { data, error } = await api.GET("/components/{name}/events", { params: { path: { name }, query: eventQuery(f) } });
  if (error) throw error;
  return { component: data?.component ?? name, events: (data?.events ?? []) as ComponentEvent[] };
}

// formatAttributes renders an event's structured payload as a compact JSON snippet
// for the row's disclosure. Returns null when the occurrence carried no payload (so
// the panel omits the disclosure entirely) or when the value cannot be stringified.
export function formatAttributes(attributes: unknown): string | null {
  if (attributes === undefined || attributes === null) return null;
  try {
    return JSON.stringify(attributes, null, 2);
  } catch {
    return null;
  }
}
