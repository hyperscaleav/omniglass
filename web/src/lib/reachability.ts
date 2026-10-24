import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { share, spans } from "./timeline";

// The reachability data layer: a thin typed wrapper over the generated client
// plus the pure read-time derivations the panel renders. The API returns the raw
// verdict state, the probe-layer signals, and the recent transition history; the
// 4-state verdict word, the availability strip, the uptime hint, and the "why"
// reason line are all derived here from those real fields (never invented). Shapes
// follow the OpenAPI (see api/reachability.go).
//
// The strip itself is not reachability's own: transitions-to-spans lives in
// lib/timeline, shared with the health history, so both surfaces read a recorded
// edge the same way.

// stalenessWindowMs mirrors v2's ~150s: a verdict older than this is stale, its
// value no longer trusted as live.
export const STALENESS_WINDOW_MS = 150_000;

export type ReachVerdict = { value: string; ts: string };
export type ReachLayer = { layer: string; check: string; value: number; detail?: string; ts: string };
export type ReachHistory = { ts: string; value: string };
export type ReachEndpoint = {
  // The endpoint's uuid, the address its custom methods take (:retry, #1059).
  // Always on the wire; optional here so a fixture that never retries need not
  // mint one.
  id?: string;
  endpoint: string;
  // The friendly string an operator reads for this endpoint. The row's
  // identity field is called `endpoint`, not `name`, so a caller adapts it to
  // the structural `Labelled` shape before rendering it.
  label?: string;
  transport: string;
  address?: string;
  node?: string;
  // When ingest last landed a batch this endpoint's task delivered, by the
  // server's clock, whatever the batch carried (#933). The one freshness
  // every endpoint has, a driver endpoint's included.
  last_seen?: string;
  verdict: ReachVerdict | null;
  // The layer-7 rungs (#812): responds (the protocol itself answered) and,
  // only when a credential was tried, auth. Absent until a probe climbs them.
  responsive?: ReachVerdict | null;
  authenticated?: ReachVerdict | null;
  // The held session, when the endpoint has held one (#1059): its latest
  // state, when it changed, and why (lib/session reads it).
  session?: components["schemas"]["ReachSessionBody"];
  // The gate a terminal refusal holds: its rung and class (ADR-0158), and
  // for a changed key the pinned and offered keys (#1035).
  rejection?: components["schemas"]["ReachRejectionBody"];
  // A tls endpoint's handshake and certificate (#1035): lib/tls reads it.
  tls?: components["schemas"]["ReachTLSBody"];
  layers: ReachLayer[];
  history: ReachHistory[];
};
export type Reachability = { component: string; endpoints: ReachEndpoint[] };

export const REACHABILITY_KEY = (name: string) => ["reachability", name] as const;

export async function getReachability(name: string): Promise<Reachability> {
  const { data, error } = await api.GET("/components/{name}/reachability", { params: { path: { name } } });
  if (error) throw error;
  return data as Reachability;
}

// A verdict word is the read-time 4-state derivation the pill renders. responding
// = latest verdict up and fresh; down = down and fresh; stale = a verdict exists
// but is older than the staleness window; unknown = no verdict yet.
export type VerdictWord = "responding" | "down" | "stale" | "unknown";

export function verdictWord(v: ReachVerdict | null, now: number = Date.now()): VerdictWord {
  if (!v) return "unknown";
  const age = now - new Date(v.ts).getTime();
  if (age > STALENESS_WINDOW_MS) return "stale";
  return v.value === "up" ? "responding" : "down";
}

// lastSeenAt is when the endpoint last delivered anything: the server's
// last_seen stamp. Nothing a sample carries will do. The verdict is stored
// only when it changes, so its ts is the last TRANSITION (#931); a driver's
// instanced series is not stamped with the endpoint, an unchanged property
// writes no row, and a node's clock can date a sample ahead (#933). Null
// before any stamp.
export function lastSeenAt(iface: ReachEndpoint): string | null {
  return iface.last_seen ?? null;
}

// endpointWord is the pill's word for one endpoint: the verdict's value, aged
// by last_seen. Before any stamp it ages by the verdict's own time, which can
// only read older than the truth, never fresher.
export function endpointWord(iface: ReachEndpoint, now: number = Date.now()): VerdictWord {
  if (!iface.verdict) return "unknown";
  return verdictWord({ value: iface.verdict.value, ts: lastSeenAt(iface) ?? iface.verdict.ts }, now);
}

// A segment of the availability strip: one contiguous stretch in a single state,
// with its share of the window (0..1) so the strip renders as flex weights.
export type Segment = { value: string; weight: number };

// segments builds the availability strip from the transition history over a
// window ending at now, through the shared timeline primitive. The strip only
// needs the state and its share of the window, so the span's wall-clock bounds are
// dropped here (the health history keeps them, to say how long a state lasted).
export function segments(history: ReachHistory[], verdict: ReachVerdict | null, now: number = Date.now()): Segment[] {
  return spans(history, verdict?.value ?? null, now).map((s) => ({ value: s.value, weight: s.weight }));
}

// uptime is the fraction of the window spent up, as a whole-number percent, for
// the "N% up" hint. Derived from the same spans the strip renders.
export function uptime(history: ReachHistory[], verdict: ReachVerdict | null, now: number = Date.now()): number | null {
  return share(spans(history, verdict?.value ?? null, now), (v) => v === "up");
}

// reason is the "why" line for a down interface: it reads the layer pattern to
// explain the failure. A host that answers ping but refuses the port is a service
// fault on a live box; a host that fails ping is unreachable outright. Returns an
// empty string when the interface is not down or the layers do not explain it.
export function reason(iface: ReachEndpoint, now: number = Date.now()): string {
  if (endpointWord(iface, now) !== "down") return "";
  const ping = iface.layers.find((l) => l.check === "icmp-reachable");
  const port = iface.layers.find((l) => l.check === "tcp-open");
  if (ping && ping.value >= 1 && port && port.value < 1) {
    return "Host answers ping but the control port is refused: service down, box up.";
  }
  if (ping && ping.value >= 1 && !port && iface.responsive?.value === "down") {
    // A udp endpoint (#1037): no port to open, so the rung is the request.
    return "Host answers ping, but the endpoint's request goes unanswered: an open udp port cannot be detected, so a request answered is the transport rung. The service is down, the port is closed, or the datagrams are lost.";
  }
  if (ping && ping.value < 1) {
    return "Host does not answer ping: unreachable on the network.";
  }
  if (port && port.value < 1) {
    return "The control port is refused or timing out.";
  }
  return "";
}

// layerWord renders a probe layer's boolean signal as its per-layer word.
export function layerWord(l: ReachLayer): string {
  const up = l.value >= 1;
  if (l.check === "tcp-open") return up ? "open" : "closed";
  return up ? "up" : "down";
}
