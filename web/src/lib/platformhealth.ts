import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { nodeStatus, type Node, type NodeStatus } from "./nodes";
import { durationText } from "./timeline";
import { describeError } from "./format";

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
export type PlatformPipeline = components["schemas"]["PlatformPipelineBody"];
export type PlatformPipelineHop = components["schemas"]["PlatformPipelineHopBody"];
export type PlatformPipelineComponent = components["schemas"]["PlatformPipelineComponentBody"];
export type PlatformPipelineFigure = components["schemas"]["PlatformPipelineFigureBody"];
export type PipelineKind = components["schemas"]["PlatformPipelineKindBody"]["kind"];
// The kind selector's choice: every kind summed, or one.
export type PipelineKindChoice = PipelineKind | "all";

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
  const value = Number.isInteger(f.value) ? String(f.value) : String(Math.round(f.value * 100) / 100);
  return col.unit ? `${value} ${col.unit}` : value;
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
