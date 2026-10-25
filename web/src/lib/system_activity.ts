// The system-scoped activity reads (#793): the events and log lines behind
// the workspace's Events and Logs tabs. One call each for the room's whole
// story, rows labeled by owner; the server merges and caps.
import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { logQuery, type LogFilter } from "./logs";

export type SystemEvent = components["schemas"]["SystemEventBody"];
export type SystemLog = components["schemas"]["SystemLogBody"];

export const systemEventsKey = (system: string) => ["system-events", system] as const;
export const systemLogsKey = (system: string, f: LogFilter = {}) => ["system-logs", system, f.severity ?? "", f.source ?? ""] as const;

// SystemLogs is the members' lines under the filter, with the sources the
// window carries whatever the filter (#1145).
export type SystemLogs = { logs: SystemLog[]; sources: string[] };

export async function systemEvents(system: string): Promise<SystemEvent[]> {
  const res = await api.GET("/systems/{name}/events", { params: { path: { name: system } } });
  if (res.error) throw res.error;
  return (res.data?.events ?? []) as SystemEvent[];
}

export async function systemLogs(system: string, f: LogFilter = {}): Promise<SystemLogs> {
  const res = await api.GET("/systems/{name}/logs", { params: { path: { name: system }, query: logQuery(f) } });
  if (res.error) throw res.error;
  return { logs: (res.data?.logs ?? []) as SystemLog[], sources: res.data?.sources ?? [] };
}
