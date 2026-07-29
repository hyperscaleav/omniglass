import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The logs data layer: a thin typed wrapper over the generated client for a
// component's recent raw log lines (ADR-0066, the ingest lane, distinct from the
// typed event sink). A log line is untyped text a rule may later derive an event
// from; most never do. The API returns them newest first (last 24 hours, capped),
// so this layer is pure I/O over the generated client and unit-testable against a
// mocked fetch. The row type is the generated LogBody, never hand-typed.

export type ComponentLog = components["schemas"]["LogBody"];
export type ComponentLogs = { component: string; logs: ComponentLog[] };

export const LOGS_KEY = (name: string) => ["logs", name] as const;

export async function getLogs(name: string): Promise<ComponentLogs> {
  const { data, error } = await api.GET("/components/{name}/logs", { params: { path: { name } } });
  if (error) throw error;
  return { component: data?.component ?? name, logs: (data?.logs ?? []) as ComponentLog[] };
}

// A node's own self-logs (ADR-0066): the same raw log line, owner-bound to the
// node instead of a component. The node's operational story (connected, worklist
// pulled, a task skipped) shipped back over the telemetry lane.
export type NodeLogs = { node: string; logs: ComponentLog[] };

export const NODE_LOGS_KEY = (name: string) => ["node-logs", name] as const;

export async function getNodeLogs(name: string): Promise<NodeLogs> {
  const { data, error } = await api.GET("/nodes/{name}/logs", { params: { path: { name } } });
  if (error) throw error;
  return { node: data?.node ?? name, logs: (data?.logs ?? []) as ComponentLog[] };
}

// severityVariant maps a syslog-style severity onto a daisyUI badge variant, so an
// error or warning line reads at a glance. Anything unclassified stays neutral.
export function severityVariant(severity: string | undefined): string {
  switch ((severity ?? "").toLowerCase()) {
    case "emerg":
    case "alert":
    case "crit":
    case "err":
    case "error":
      return "badge-error";
    case "warning":
    case "warn":
      return "badge-warning";
    default:
      return "badge-ghost";
  }
}
