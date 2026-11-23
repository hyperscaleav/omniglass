// The overlay timeline's reads (#2425): everything a component or a system
// answers over one [from, to], each read honoring the range on the server
// rather than trimming a fixed window in the browser. Health and alarms come
// from their routes' since/until; events, logs and property changes through
// the views binding's from/to (the typed event and log routes are fixed at a
// day); commands from the commands route's since/until. A read that stops at a
// cap says so (`more`), so the timeline never truncates silently.
import { api } from "../api/client";
import type { Alarm } from "./alarms";
import type { Command } from "./commands";
import { runView, type ViewBinding } from "./binding";
import type { ViewEventBody, ViewLogBody, ViewPropertySeriesBody } from "../api/api.gen";
import type { LogSeverity } from "./logs";

export type TimelineKind = "components" | "systems";
const iso = (t: number) => new Date(t).toISOString();

export type HealthOver = { health: string; carriedIn?: string; transitions: { ts: string; health: string }[] };

export const healthOverKey = (kind: TimelineKind, id: string, from: number, to: number) => ["timeline-health", kind, id, from, to] as const;

export async function healthOver(kind: TimelineKind, id: string, from: number, to: number): Promise<HealthOver> {
  const query = { since: iso(from), until: iso(to) };
  const res =
    kind === "components"
      ? await api.GET("/components/{name}/health", { params: { path: { name: id }, query } })
      : await api.GET("/systems/{name}/health", { params: { path: { name: id }, query } });
  if (res.error) throw res.error;
  const d = res.data!;
  return { health: d.health, carriedIn: d.carried_in || undefined, transitions: d.transitions ?? [] };
}

export const alarmsOverKey = (kind: TimelineKind, id: string, from: number, to: number) => ["timeline-alarms", kind, id, from, to] as const;

// The system read caps at 500 alarms; a component's is the component's own.
export const SYSTEM_ALARM_CAP = 500;

export async function alarmsOver(kind: TimelineKind, id: string, from: number, to: number): Promise<{ alarms: Alarm[]; more: boolean }> {
  if (kind === "components") {
    const res = await api.GET("/components/{name}/alarms", { params: { path: { name: id }, query: { include_cleared: true, since: iso(from), until: iso(to) } } });
    if (res.error) throw res.error;
    return { alarms: (res.data?.alarms ?? []) as Alarm[], more: false };
  }
  const res = await api.GET("/systems/{name}/alarms", { params: { path: { name: id }, query: { since: iso(from), until: iso(to) } } });
  if (res.error) throw res.error;
  const alarms = (res.data?.alarms ?? []) as Alarm[];
  return { alarms, more: alarms.length >= SYSTEM_ALARM_CAP };
}

export const COMMAND_CAP = 500;
export const commandsOverKey = (component: string, from: number, to: number) => ["timeline-commands", component, from, to] as const;

export async function commandsOver(component: string, from: number, to: number): Promise<{ commands: Command[]; more: boolean }> {
  const res = await api.GET("/components/{name}/commands", {
    params: { path: { name: component }, query: { since: iso(from), until: iso(to), limit: COMMAND_CAP } },
  });
  if (res.error) throw res.error;
  const commands = (res.data?.commands ?? []) as Command[];
  return { commands, more: commands.length >= COMMAND_CAP };
}

// Rows through the views binding come a page of at most 1000 at a time, newest
// first; the timeline reads up to ROW_PAGES pages and says when more remain.
export const ROW_PAGES = 3;
const PAGE = 1000;

async function pagedRows<T>(b: ViewBinding, pick: (r: Awaited<ReturnType<typeof runView>>) => T[] | null | undefined): Promise<{ rows: T[]; more: boolean }> {
  const rows: T[] = [];
  let page: string | undefined;
  for (let i = 0; i < ROW_PAGES; i++) {
    const r = await runView({ ...b, limit: PAGE, ...(page ? { page } : {}) });
    rows.push(...(pick(r) ?? []));
    page = r.next_page || undefined;
    if (!page) return { rows, more: false };
  }
  return { rows, more: true };
}

// A system's own events and its members' (the selector names members by the
// system filter); a component's own.
function ownerBindings(kind: TimelineKind, id: string, from: number, to: number): ViewBinding[] {
  const range = { from: iso(from), to: iso(to) };
  if (kind === "components") return [{ primitive: "event", kind: "component", refs: [id], ...range }];
  return [
    { primitive: "event", kind: "system", refs: [id], ...range },
    { primitive: "event", kind: "component", filter: { system: id }, ...range },
  ];
}

export type TimelineEvent = ViewEventBody & { owner?: string };
export const eventsOverKey = (kind: TimelineKind, id: string, from: number, to: number) => ["timeline-events", kind, id, from, to] as const;

export async function eventsOver(kind: TimelineKind, id: string, from: number, to: number): Promise<{ events: TimelineEvent[]; more: boolean }> {
  const parts = await Promise.all(ownerBindings(kind, id, from, to).map((b) => pagedRows(b, (r) => r.events)));
  return { events: parts.flatMap((p) => p.rows), more: parts.some((p) => p.more) };
}

export type TimelineLog = ViewLogBody;
export const logsOverKey = (kind: TimelineKind, id: string, from: number, to: number, severity: LogSeverity) => ["timeline-logs", kind, id, from, to, severity] as const;

// Log lines belong to components: a system's are its members'.
export async function logsOver(kind: TimelineKind, id: string, from: number, to: number, severity: LogSeverity): Promise<{ logs: TimelineLog[]; more: boolean }> {
  const b: ViewBinding =
    kind === "components"
      ? { primitive: "log", kind: "component", refs: [id], from: iso(from), to: iso(to), severity }
      : { primitive: "log", kind: "component", filter: { system: id }, from: iso(from), to: iso(to), severity };
  const { rows, more } = await pagedRows(b, (r) => r.logs);
  return { logs: rows, more };
}

// The views binding reads at most 50 keys a binding.
export const PROPERTY_KEYS_MAX = 50;
export const propertiesOverKey = (kind: TimelineKind, id: string, keys: readonly string[], from: number, to: number) =>
  ["timeline-properties", kind, id, [...keys].sort().join(","), from, to] as const;

// Every change of the named properties inside the range, with the value each
// carried in: one series per key and instance.
export async function propertiesOver(kind: TimelineKind, id: string, keys: readonly string[], from: number, to: number): Promise<ViewPropertySeriesBody[]> {
  if (keys.length === 0) return [];
  const r = await runView({
    primitive: "property",
    kind: kind === "systems" ? "system" : "component",
    refs: [id],
    keys: keys.slice(0, PROPERTY_KEYS_MAX),
    from: iso(from),
    to: iso(to),
  });
  return r.property ?? [];
}
