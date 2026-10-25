import { api } from "../api/client";
import type { components, operations } from "../api/schema.gen";

// The logs data layer: a thin typed wrapper over the generated client for a
// component's recent raw log lines (ADR-0066, the ingest lane, distinct from the
// typed event sink). A log line is untyped text a rule may later derive an event
// from; most never do. The API returns them newest first (last 24 hours, capped),
// narrowed by an optional severity threshold and source (#1145), with the sources
// the window carries whatever the filter, so this layer is pure I/O over the
// generated client and unit-testable against a mocked fetch. The row type is the
// generated LogBody, never hand-typed.

export type ComponentLog = components["schemas"]["LogBody"];
export type ComponentLogs = { component: string; logs: ComponentLog[]; sources?: string[] };

// LogSeverity is the canonical set the API's severity filter takes, from the
// generated operation, so the console cannot offer a word the gateway refuses.
export type LogSeverity = NonNullable<NonNullable<operations["list-component-logs"]["parameters"]["query"]>["severity"]>;

// LOG_SEVERITIES is RFC 5424's eight levels, most severe first. The list is
// written once here for the picker; the type below fails the typecheck if the
// generated enum gains or loses a level the list does not carry.
export const LOG_SEVERITIES = ["emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"] as const satisfies readonly LogSeverity[];
type Missing = Exclude<LogSeverity, (typeof LOG_SEVERITIES)[number]>;
const everyLevelListed: Missing extends never ? true : Missing = true;
void everyLevelListed;

// LogFilter is the read's narrowing: a severity threshold (that level and every
// more severe one) and an exact source, either empty for all.
export type LogFilter = { severity?: LogSeverity | ""; source?: string };

const NO_FILTER: LogFilter = {};

export const LOGS_KEY = (name: string, f: LogFilter = NO_FILTER) => ["logs", name, f.severity ?? "", f.source ?? ""] as const;

// logQuery renders a filter as the read's query parameters, leaving out what is
// empty so an unfiltered read asks for nothing.
export function logQuery(f: LogFilter = NO_FILTER): { severity?: LogSeverity; source?: string } {
  const q: { severity?: LogSeverity; source?: string } = {};
  if (f.severity) q.severity = f.severity;
  if (f.source) q.source = f.source;
  return q;
}

export async function getLogs(name: string, f: LogFilter = NO_FILTER): Promise<ComponentLogs> {
  const { data, error } = await api.GET("/components/{name}/logs", { params: { path: { name }, query: logQuery(f) } });
  if (error) throw error;
  return { component: data?.component ?? name, logs: (data?.logs ?? []) as ComponentLog[], sources: data?.sources ?? [] };
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

// severityVariant maps a severity onto a daisyUI badge variant, so an error or a
// warning line reads at a glance: the canonical RFC 5424 words a component's line
// carries (#1145), and the short and slog spellings a node's self-logs use.
// Anything else stays neutral.
export function severityVariant(severity: string | undefined): string {
  switch ((severity ?? "").toLowerCase()) {
    case "emergency":
    case "emerg":
    case "alert":
    case "critical":
    case "crit":
    case "error":
    case "err":
      return "badge-error";
    case "warning":
    case "warn":
      return "badge-warning";
    default:
      return "badge-ghost";
  }
}

// arrivedApart reports a line whose own time and its arrival differ by more than
// a minute (#1145): a driver read the time out of the line, and the console shows
// both so a backfilled or a clock-skewed line is not taken for a fresh one.
export function arrivedApart(line: Pick<ComponentLog, "ts" | "received_at">): boolean {
  if (!line.received_at) return false;
  return Math.abs(Date.parse(line.ts) - Date.parse(line.received_at)) > 60_000;
}
