import { api } from "../api/client";
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
export type ReachInterface = {
  interface: string;
  interface_type: string;
  endpoint?: string;
  node?: string;
  verdict: ReachVerdict | null;
  layers: ReachLayer[];
  history: ReachHistory[];
};
export type Reachability = { component: string; interfaces: ReachInterface[] };

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
export function reason(iface: ReachInterface, now: number = Date.now()): string {
  if (verdictWord(iface.verdict, now) !== "down") return "";
  const ping = iface.layers.find((l) => l.check === "icmp.reachable");
  const port = iface.layers.find((l) => l.check === "tcp.open");
  if (ping && ping.value >= 1 && port && port.value < 1) {
    return "Host answers ping but the control port is refused: service down, box up.";
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
  if (l.check === "tcp.open") return up ? "open" : "closed";
  return up ? "up" : "down";
}
