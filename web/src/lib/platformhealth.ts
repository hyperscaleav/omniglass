import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { nodeStatus, type Node, type NodeStatus } from "./nodes";
import { durationText } from "./timeline";
import { describeError } from "./format";
import { humanSize } from "./files";

// The Platform health data layer (#1644): the one read behind the page, and the
// pure derivations it renders. Every figure is a sample on a platform component
// the fleet already has; nothing here invents one. Two rules hold throughout:
//
//   - The page has no key list. The columns are the groups the view returns
//     (storage.PlatformViewGroups on the server), so a key added there is a
//     column here with no web change.
//   - One clock. Uptime and the liveness pill are both judged against the view's
//     read_at, the server's clock at the read, so the header and the rows never
//     disagree about what "now" is.

export type PlatformView = components["schemas"]["PlatformViewOutputBody"];
export type PlatformNode = components["schemas"]["PlatformNodeBody"];
export type PlatformServer = components["schemas"]["PlatformServerBody"];
export type PlatformPanel = components["schemas"]["PlatformPanelBody"];
export type PlatformPanelKey = components["schemas"]["PlatformPanelKeyBody"];
export type PlatformAlarm = components["schemas"]["PlatformAlarmBody"];
export type PlatformInstanceFigure = components["schemas"]["PlatformInstanceFigureBody"];

export const PLATFORM_VIEW_KEY = ["platform-view"] as const;

export async function platformView(): Promise<PlatformView> {
  const { data, error } = await api.GET("/views/platform");
  if (error) throw error;
  return data as PlatformView;
}

// The view refuses a node read short of all-scope (403): the platform's rows
// are every node, so a read held for some places cannot open it. That is an
// answer the server gives again on every retry, so a 4xx is never retried and
// the caller hears it at once; a 5xx or a dropped connection is retried like
// any read, up to the query library's usual three.
export function retryPlatformView(failures: number, err: unknown): boolean {
  const status = (err as { status?: number } | null)?.status;
  if (typeof status === "number" && status >= 400 && status < 500) return false;
  return failures < 3;
}

export const PLATFORM_REFUSED = "Platform health needs a fleet-wide node read; yours reaches only part of the fleet.";

// platformRefusal is the line an error reads as: a 403 names the read it lacks
// (the server's own detail is a bare "forbidden"), anything else is the
// error's own message.
export function platformRefusal(err: unknown): string {
  if ((err as { status?: number } | null)?.status === 403) return PLATFORM_REFUSED;
  return describeError(err);
}

// One node-table column: a key, the group it sits under, and the catalog's own
// label, unit and description (the header tooltip, ADR-0127).
export type PlatformColumn = { key: string; group: string; label: string; unit: string; description: string };

export function platformColumns(v: PlatformView): PlatformColumn[] {
  return (v.groups ?? []).flatMap((g) =>
    (g.keys ?? []).map((k) => ({ key: k.name, group: g.label, label: k.label, unit: k.unit ?? "", description: k.description ?? "" })),
  );
}

// serverUptime is read_at minus started_at, null before a boot has written
// its start time.
export function serverUptime(readAt: string, startedAt: string | undefined): string | null {
  if (!startedAt) return null;
  return durationText(Date.parse(readAt) - Date.parse(startedAt));
}

export const NOT_REPORTED = "not reported";

// figureText is one cell. A key the node never reported, or a row the caller
// may not read figures for, is "not reported": a zero would claim a reading
// that never happened.
export function figureText(n: PlatformNode, col: Pick<PlatformColumn, "key" | "unit">): string {
  const f = n.figures?.[col.key];
  if (!f) return NOT_REPORTED;
  return valueText(f.value, col.unit);
}

// valueText is a figure with its unit: an integer as is, anything else trimmed
// to two places.
function valueText(value: number, unit: string): string {
  if (unit === "bytes") return humanSize(value);
  const v = Number.isInteger(value) ? String(value) : String(Math.round(value * 100) / 100);
  return unit ? `${v} ${unit}` : v;
}

// nodePill is the Nodes page's own liveness rule, evaluated at read_at.
export function nodePill(n: PlatformNode, readAt: string): NodeStatus {
  return nodeStatus({ ...n, tags: {} } as Node, Date.parse(readAt));
}

// notUpCount is how many nodes are down or have never checked in: the Home
// tile's second figure.
export function notUpCount(v: PlatformView): number {
  return (v.nodes ?? []).filter((n) => nodePill(n, v.read_at) !== "up").length;
}

// A server's panels (#1645). Each panel is a group the view returns, and each
// key in it a section of rows: one per instance the view sent, in the order it
// sent them. A ranked key's rows are already the server's cut (its newest
// window, highest first), so the page ranks nothing and filters nothing. A key
// another key is read against (the pool's size under connections in use)
// prints as the second half of that key's pair, not as a row of its own.

// The panels' sparklines read the last hour at one-minute buckets: long enough
// to see a lag build and drain, short enough that a minute is a visible step.
export const PANEL_SERIES_HOURS = 1;
export const PANEL_SERIES_RESOLUTION_S = 60;

export type PanelRow = {
  key: string;
  // The series instance; "" for an unsplit key, the owner-level series.
  instance: string;
  unit: string;
  // null when the key has never been sampled here: not reported, never 0.
  value: number | null;
  observedAt?: string;
  of?: { key: string; unit: string; value: number | null };
};

export type PanelSection = { key: PlatformPanelKey; ranked: boolean; rows: PanelRow[] };

export function panelSections(panel: PlatformPanel): PanelSection[] {
  const keys = panel.keys ?? [];
  const figures = panel.figures ?? [];
  const byKey = new Map<string, PlatformInstanceFigure[]>();
  for (const f of figures) {
    const list = byKey.get(f.key) ?? [];
    list.push(f);
    byKey.set(f.key, list);
  }
  const seconds = new Set(keys.map((k) => k.of).filter((o): o is string => !!o));
  const keyByName = new Map(keys.map((k) => [k.name, k]));
  return keys
    .filter((k) => !seconds.has(k.name))
    .map((k) => {
      const of = k.of ? keyByName.get(k.of) : undefined;
      const ofFigures = k.of ? (byKey.get(k.of) ?? []) : [];
      // A pair's second figure is the same instance's, or the second key's
      // sole series when it is split differently (the retention window is the
      // metric lane's, the oldest day the owner's).
      const ofValue = (instance: string): number | null =>
        (ofFigures.find((f) => f.instance === instance) ?? (ofFigures.length === 1 ? ofFigures[0] : undefined))?.value ?? null;
      const pair = (instance: string) =>
        k.of ? { of: { key: k.of, unit: of?.unit ?? "", value: ofValue(instance) } } : {};
      const own = byKey.get(k.name) ?? [];
      const rows: PanelRow[] = own.length
        ? own.map((f) => ({ key: k.name, instance: f.instance, unit: k.unit ?? "", value: f.value, observedAt: f.observed_at, ...pair(f.instance) }))
        : [{ key: k.name, instance: "", unit: k.unit ?? "", value: null, ...pair("") }];
      return { key: k, ranked: (k.rank ?? 0) > 0, rows };
    });
}

// panelRowText is a row's figure: its value and unit, a pair as "a / b" (one
// unit, after both, when they share it), a key never sampled as not reported.
export function panelRowText(row: PanelRow): string {
  if (row.value === null) return NOT_REPORTED;
  if (!row.of) return valueText(row.value, row.unit);
  // A shared unit is said once, after both, unless it scales each side on
  // its own (bytes): "4 / 20", "1.0 MB / 4.0 MB".
  const unit = row.unit && row.unit === row.of.unit && row.unit !== "bytes" ? row.unit : "";
  const b = row.of.value === null ? "?" : valueText(row.of.value, row.of.unit === unit ? "" : row.of.unit);
  const a = valueText(row.value, row.unit === unit ? "" : row.unit);
  return unit ? `${a} / ${b} ${unit}` : `${a} / ${b}`;
}

export type PanelSeriesRequest = {
  kind: "components";
  owner: string;
  metric: string;
  hours: number;
  resolutionSeconds: number;
  instance: string;
};

// panelSeriesRequest is a row's sparkline read: its key's series on the
// server's platform component. The instance is always named, the empty one
// included: left unset, the read answers 422 for a key with several series.
export function panelSeriesRequest(server: PlatformServer, row: PanelRow): PanelSeriesRequest {
  return {
    kind: "components",
    owner: server.component,
    metric: row.key,
    hours: PANEL_SERIES_HOURS,
    resolutionSeconds: PANEL_SERIES_RESOLUTION_S,
    instance: row.instance,
  };
}

// panelAlarms are the server's open alarms raised on one of the panel's keys
// (the raising rule's sample key): the page draws the flag from the alarms
// the view carries, never from a threshold of its own.
export function panelAlarms(server: PlatformServer, panel: PlatformPanel): PlatformAlarm[] {
  const keys = new Set((panel.keys ?? []).map((k) => k.name));
  return (server.alarms ?? []).filter((a) => !!a.key && keys.has(a.key));
}

export function panelFlagged(server: PlatformServer, panel: PlatformPanel): boolean {
  return panelAlarms(server, panel).length > 0;
}
