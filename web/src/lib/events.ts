import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The events data layer: a thin typed wrapper over the generated client for a
// component's recent log-kind observations. Where reachability samples a value on
// a cadence (a metric or a state), the event sink collects discrete occurrences:
// syslog lines, traps, and the like, each a message stamped when it happened. The
// API returns them newest first (last 24 hours, capped), so this layer is pure I/O
// over the generated client and unit-testable against a mocked fetch. Shapes follow
// the OpenAPI (see api/events.go); the row type is the generated EventBody, never
// hand-typed.

export type ComponentEvent = components["schemas"]["EventBody"];
export type ComponentEvents = { component: string; events: ComponentEvent[] };

export const EVENTS_KEY = (name: string) => ["events", name] as const;

export async function getEvents(name: string): Promise<ComponentEvents> {
  const { data, error } = await api.GET("/components/{name}/events", { params: { path: { name } } });
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
