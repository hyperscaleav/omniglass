// The system-scoped activity reads (#793): the events and log lines behind
// the workspace's Events and Logs tabs. One call each for the room's whole
// story, rows labeled by owner; the server merges and caps.
import { api } from "../api/client";
import type { components } from "../api/schema.gen";

export type SystemEvent = components["schemas"]["SystemEventBody"];
export type SystemLog = components["schemas"]["SystemLogBody"];

export const systemEventsKey = (system: string) => ["system-events", system] as const;
export const systemLogsKey = (system: string) => ["system-logs", system] as const;

export async function systemEvents(system: string): Promise<SystemEvent[]> {
  const res = await api.GET("/systems/{name}/events", { params: { path: { name: system } } });
  if (res.error) throw res.error;
  return (res.data?.events ?? []) as SystemEvent[];
}

export async function systemLogs(system: string): Promise<SystemLog[]> {
  const res = await api.GET("/systems/{name}/logs", { params: { path: { name: system } } });
  if (res.error) throw res.error;
  return (res.data?.logs ?? []) as SystemLog[];
}
