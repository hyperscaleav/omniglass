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

// ---- With a simulator (#1647) ----
//
// The simulation section reads what each node's simulator saw against what the
// platform collected, over the sliding window the server brackets each minute,
// a lab faster (internal/closedloop). Every gap is a loss ratio, 0 healthy. The derivations
// below decide only order and tone; every count is the server's.

export type PlatformSimulation = components["schemas"]["PlatformSimulationViewOutputBody"];
export type PlatformSimNode = components["schemas"]["PlatformSimulationNode"];
export type PlatformSimGap = components["schemas"]["PlatformSimulationGap"];
export type PlatformSimDevice = components["schemas"]["PlatformSimulationDevice"];

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
  return [...(devices ?? [])].sort((a, b) => rank(a) - rank(b) || (a.name || a.component_id).localeCompare(b.name || b.component_id));
}
