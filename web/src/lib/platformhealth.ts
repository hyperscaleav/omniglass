import { api } from "../api/client";
import type {
  PlatformAlarmBody,
  PlatformInstanceFigureBody,
  PlatformNodeBody,
  PlatformPanelBody,
  PlatformPanelKeyBody,
  PlatformPipelineBody,
  PlatformPipelineComponentBody,
  PlatformPipelineFigureBody,
  PlatformPipelineHopBody,
  PlatformPipelineKindBody,
  PlatformServerBody,
  PlatformSimulationDevice,
  PlatformSimulationGap,
  PlatformSimulationNode,
  PlatformSimulationViewOutputBody,
  PlatformViewOutputBody,
} from "../api/api.gen";
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

export type PlatformView = PlatformViewOutputBody;
export type PlatformNode = PlatformNodeBody;
export type PlatformServer = PlatformServerBody;
export type PlatformPipeline = PlatformPipelineBody;
export type PlatformPipelineHop = PlatformPipelineHopBody;
export type PlatformPipelineComponent = PlatformPipelineComponentBody;
export type PlatformPipelineFigure = PlatformPipelineFigureBody;
export type PipelineKind = PlatformPipelineKindBody["kind"];
// The kind selector's choice: every kind summed, or one.
export type PipelineKindChoice = PipelineKind | "all";
export type PlatformPanel = PlatformPanelBody;
export type PlatformPanelKey = PlatformPanelKeyBody;
export type PlatformAlarm = PlatformAlarmBody;
export type PlatformInstanceFigure = PlatformInstanceFigureBody;

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

// ---- With a simulator (#1647) ----
//
// The simulation section reads what each node's simulator saw against what the
// platform collected, over the sliding window the server brackets each minute,
// a lab faster (internal/closedloop). Every gap is a loss ratio, 0 healthy. The derivations
// below decide only order and tone; every count is the server's.

export type PlatformSimulation = PlatformSimulationViewOutputBody;
export type PlatformSimNode = PlatformSimulationNode;
export type PlatformSimGap = PlatformSimulationGap;
export type PlatformSimDevice = PlatformSimulationDevice;

export const PLATFORM_SIM_KEY = ["platform-simulation"] as const;

export async function platformSimulation(): Promise<PlatformSimulation> {
  const { data, error } = await api.GET("/views/platform/simulation");
  if (error) throw error;
  return data as PlatformSimulation;
}

// sortGaps puts the worst ratio first. A gap with no ratio (nothing went in,
// the sim does not observe the hop, or the node restarted) sinks below every
// measured one; ties keep the server's travel order.
export function sortGaps(gaps: PlatformSimGap[] | null | undefined): PlatformSimGap[] {
  const rank = (g: PlatformSimGap) => (g.ratio == null ? -1 : g.ratio);
  return [...(gaps ?? [])].sort((a, b) => rank(b) - rank(a));
}

// A gap's tone is how it reads, not how big it is. Loss above 0 is a fault
// nothing on the page explains; impaired above 0 is the expected cost of an
// impairment someone applied; restarted is neither, a window no figure from is
// meaningful. A 0, or a hop with nothing in, is calm: a ratio is tinted only
// above 0.
export type GapTone = "fault" | "expected" | "restarted" | "calm";

export function gapTone(g: Pick<PlatformSimGap, "reading" | "ratio">): GapTone {
  if (g.reading === "restarted") return "restarted";
  if (g.ratio == null || g.ratio <= 0) return "calm";
  if (g.reading === "impaired") return "expected";
  if (g.reading === "loss") return "fault";
  return "calm";
}

// gapName is a gap's hop as words: "sent-not-received" reads "sent, not received".
export function gapName(gap: string): string {
  return gap.replace("-not-", ", not ").replaceAll("-", " ");
}

// gapProtocolText names what a gap row counts. A connect gap with no protocol
// pools every listener on the node (the node knows the transport it dialed,
// the sim the listener that accepted); the telemetry hops count batches.
export function gapProtocolText(g: Pick<PlatformSimGap, "gap" | "protocol">): string {
  if (g.protocol) return g.protocol;
  return g.gap === "connect-not-accepted" ? "any listener" : "batches";
}

// ratioText is a loss ratio as a percent; a gap with no ratio has no number.
export function ratioText(ratio: number | null | undefined): string {
  if (ratio == null) return "-";
  if (ratio === 0) return "0%";
  const pct = ratio * 100;
  return `${pct < 0.1 ? pct.toFixed(2) : pct.toFixed(1)}%`;
}

// windowText is the window's length in words: whole minutes, or seconds for a
// lab's short window that is not a whole number of them. Unread, it is the
// server's default ten minutes.
export function windowText(seconds: number | null | undefined): string {
  const s = seconds ?? 600;
  if (s % 60 !== 0) return `${s} seconds`;
  const m = s / 60;
  return m === 1 ? "1 minute" : `${m} minutes`;
}

export const WARMING_UP =
  "Warming up: the server needs a full window and the bracket after it before it reads a gap.";
export const RESTARTED =
  "Restarted: a counter went backwards inside the window, so no figure from it means anything until the window slides past.";

// simStateNote is the reason a node's window carries no figures, or null when
// it is measured. It names no number: a count while warming up would be a
// partial window read as a whole one.
export function simStateNote(n: Pick<PlatformSimNode, "state">): string | null {
  if (n.state === "warming up") return WARMING_UP;
  if (n.state === "restarted") return RESTARTED;
  return null;
}

export const NOT_WIRED = "not wired yet";

// wiredText is a figure the node does not report yet (its dispatch queue depth
// and busy workers): no number until it does.
export function wiredText(v: number | null | undefined): string {
  return v == null ? NOT_WIRED : String(v);
}

// deviceReceived is a device's requests over the window across protocols.
export function deviceReceived(d: Pick<PlatformSimDevice, "received">): number {
  return Object.values(d.received ?? {}).reduce((a, b) => a + b, 0);
}

// sortDevices puts the devices that need a look first: unhit, then impaired,
// then by name.
export function sortDevices(devices: PlatformSimDevice[] | null | undefined): PlatformSimDevice[] {
  const rank = (d: PlatformSimDevice) => (d.unhit ? 0 : d.impaired ? 1 : 2);
  return [...(devices ?? [])].sort((a, b) => rank(a) - rank(b) || (a.component || a.component_id).localeCompare(b.component || b.component_id));
}

// Where work is lost (#1646). Every figure is a loss ratio: the share of what a
// hop took in that it did not hand on, 0 is healthy. The page holds no hop
// list: the hops are the view's, in the order an item crosses them.

// The delivery model's kind order (delivery.Kinds on the server).
export const PIPELINE_KINDS: PipelineKind[] = ["metric", "property", "event", "log"];

// lossPercent is a ratio as a percent with two significant figures, so a
// small loss (0.0002) reads 0.02% and is never rounded to a healthy 0.
export function lossPercent(r: number): string {
  if (r === 0) return "0%";
  return `${Number((r * 100).toPrecision(2))}%`;
}

// lossTinted is the one tint rule: a ratio above 0 draws the eye, 0 and an
// absent ratio do not.
export function lossTinted(r: number | undefined): boolean {
  return r !== undefined && r > 0;
}

// pickFigure reads a span's last hour for every kind (the total) or one kind's
// row; absent when the hop or component carried nothing of the kind, or
// carried nothing at all.
function pickFigure(
  kinds: { kind: PipelineKind; last_hour: PlatformPipelineFigure }[] | null,
  total: { last_hour: PlatformPipelineFigure },
  kind: PipelineKindChoice,
): PlatformPipelineFigure | undefined {
  const rows = kinds ?? [];
  if (kind === "all") return rows.length > 0 ? total.last_hour : undefined;
  return rows.find((k) => k.kind === kind)?.last_hour;
}

export function hopFigure(h: PlatformPipelineHop, kind: PipelineKindChoice): PlatformPipelineFigure | undefined {
  return pickFigure(h.kinds, h.total, kind);
}

export function componentFigure(c: PlatformPipelineComponent, kind: PipelineKindChoice): PlatformPipelineFigure | undefined {
  return pickFigure(c.kinds, c.total, kind);
}

// pipelineKinds is the kinds any hop carried, in the delivery model's order:
// the selector offers no kind the pipeline has never seen.
export function pipelineKinds(p: PlatformPipeline): PipelineKind[] {
  const seen = new Set((p.hops ?? []).flatMap((h) => (h.kinds ?? []).map((k) => k.kind)));
  return PIPELINE_KINDS.filter((k) => seen.has(k));
}

// endToEndFigure is the share lost anywhere on the path over the last hour:
// 1 - the product of every hop's survival, composed on the server.
export function endToEndFigure(p: PlatformPipeline, kind: PipelineKindChoice): number | undefined {
  if (kind === "all") return p.end_to_end_total.last_hour;
  return (p.end_to_end ?? []).find((e) => e.kind === kind)?.last_hour;
}

// nodeLossText is a node's Loss cell: its composed ratio, or not reported for
// a node whose own hops took nothing in (never a fake 0).
export function nodeLossText(n: PlatformNode): string {
  return n.loss === undefined ? NOT_REPORTED : lossPercent(n.loss);
}

// nodeLossHint is the Loss column's tooltip, naming the hops the figure
// composes, which the view says rather than the page assuming.
export function nodeLossHint(v: PlatformView): string {
  const hops = v.loss_hops ?? [];
  const named = hops.length > 1 ? `${hops.slice(0, -1).join(", ")} and ${hops[hops.length - 1]} hops` : `${hops[0] ?? "node"} hop`;
  return `The share of what this node's own ${named} took in over the last hour that it did not hand on, every kind summed and composed as the end to end is. 0% is healthy; not reported means nothing went in.`;
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
