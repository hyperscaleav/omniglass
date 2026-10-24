// The component leaf's derivations (#637): the membership rows the leaf lists
// and the collection card's state. Pure, so the one distinction the card
// exists to make is pinned with no DOM: a healthy node with a stale sample
// means the component or the path, not collection, and that sentence is why
// nodes need no zoom of their own.

import { locationIndex, type FleetView } from "./fleet";
import { sortAlarms, type Alarm } from "./alarms";
import type { SeverityLevel } from "./severity";
import type { components } from "../api/schema.gen";
import type { Member } from "./members";
import type { Node } from "./nodes";
import { nodeStatus } from "./nodes";
import type { ReachEndpoint } from "./reachability";
import { STALENESS_WINDOW_MS, endpointWord, lastSeenAt } from "./reachability";
import { pollsHeld } from "./session";
import { entityLabel } from "./entities";

// The leaf's poll (#934): a monitoring console's component view moves as
// collection arrives. The component's own reads (reachability, vitals, and
// its alarms with the verdict they make) poll at the cadence of the sibling
// live surfaces (a rule's detail, the Simulation section); the node behind
// each endpoint (usually one) at a slower cadence, its heartbeat moving no
// faster. Nothing it polls grows with the fleet: no fleet view, no node
// list. TanStack holds an interval while the tab is hidden.
export const LEAF_POLL_MS = 5_000;
export const LEAF_NODE_POLL_MS = 10_000;

export type CollectionState = {
  // collecting: fresh sample, node live. component-or-path: the sample went
  // stale while the node stayed live, so collection is fine and the component or
  // the route to it is not. node-offline: the node itself stopped
  // heartbeating, so staleness says nothing about the component. down: the
  // component answered, badly, which is a component fact and not a collection one.
  // rejected and session-down (#1059): the endpoint's held session refused or
  // gave up, so its polls stopped or fail while the probes beside them keep
  // delivering, and freshness alone would read collecting. A gate the probe
  // set with no session held reads rejected too where it holds the polls
  // (#1110, lib/session's pollsHeld).
  kind: "collecting" | "component-or-path" | "node-offline" | "down" | "rejected" | "session-down" | "unknown";
  node?: string;
};

// Collection is judged by when the endpoint last delivered anything (the
// server's last_seen), not by its reachability verdict: a driver endpoint
// never gets a verdict, yet its batches say it is collecting (#933). Before
// any stamp a verdict's own time stands in, which can only read older. Only
// a probe endpoint carries a verdict, and only its down value says more than
// freshness does.
export function collectionState(iface: ReachEndpoint, node: Node | undefined, now: number = Date.now()): CollectionState {
  const nodeDown = !node || nodeStatus(node, now) !== "up";
  const last = lastSeenAt(iface) ?? iface.verdict?.ts ?? null;
  if (!last) return { kind: nodeDown ? "node-offline" : "unknown", node: iface.node };
  if (now - new Date(last).getTime() > STALENESS_WINDOW_MS) return { kind: nodeDown ? "node-offline" : "component-or-path", node: iface.node };
  if (iface.session?.state === "rejected" || pollsHeld(iface)) return { kind: "rejected", node: iface.node };
  if (iface.session?.state === "down") return { kind: "session-down", node: iface.node };
  if (endpointWord(iface, now) === "down") return { kind: "down", node: iface.node };
  return { kind: "collecting", node: iface.node };
}

export type MembershipRow = {
  system: string;
  label: string;
  // The room the system sits in, when the view knows it: two memberships can
  // legally read the same label (each the first of its kind in its own room),
  // and the room is what tells them apart.
  where?: string;
  primary: boolean;
  // The uuid to drill to, resolved through the fleet view when the bare name
  // is unique there; null when ambiguous (two systems may legally share a
  // name under different placements) or out of the caller's view, in which
  // case the row renders undrillable rather than guessing.
  systemId: string | null;
};

export function membershipRows(members: Member[], view: FleetView): MembershipRow[] {
  const byId = new Map((view.systems ?? []).map((s) => [s.id, s]));
  const byName = new Map<string, { id: string; label: string }[]>();
  for (const s of view.systems ?? []) {
    const list = byName.get(s.name);
    const entry = { id: s.id, label: entityLabel(s) };
    if (list) list.push(entry);
    else byName.set(s.name, [entry]);
  }
  return members.map((m) => {
    // The wire carries the uuid (system_id); resolve by it, and fall back to
    // the bare name only when the wire lacks it and the name is unique.
    const direct = m.system_id ? byId.get(m.system_id) : undefined;
    if (direct) {
      const room = direct.location ? locationIndex(view).get(direct.location) : undefined;
      const label = entityLabel(direct);
      // A system named for its room (the field's commonest naming) would
      // read "Huddle Room  Huddle Room": the room only tells rows apart when
      // it says something the label does not.
      const where = room && entityLabel(room) !== label ? entityLabel(room) : undefined;
      return { system: m.system, label, where, primary: m.primary, systemId: direct.id };
    }
    const hits = byName.get(m.system) ?? [];
    return {
      system: m.system,
      label: hits.length === 1 ? hits[0].label : m.system,
      primary: m.primary,
      systemId: m.system_id ?? (hits.length === 1 ? hits[0].id : null),
    };
  });
}

// The dispatch facts (#786): what a truck roll or an RMA needs, derived
// pure from the three reads the leaf adds.

// sourceRule and sourceRuleId name the calc rule whose calculated value the
// row is, when one is (#999, #1018).
export type IdentityRow = { name: string; label: string; value: unknown; sourceRule?: string; sourceRuleId?: string };

// identityRows keeps every property that resolves to a value (an explicit
// set, a calc rule's value, or the contract default): model, serial,
// firmware are the point, but the rule is the value's presence, not a
// hand-kept key list.
export function identityRows(props: components["schemas"]["EffectivePropertyBody"][]): IdentityRow[] {
  return (props ?? [])
    .filter((p) => p.value !== null && p.value !== undefined)
    .map((p) => {
      const row: IdentityRow = { name: p.property_type_name, label: entityLabel({ name: p.property_type_name, label: p.label }), value: p.value };
      if (p.source_rule && p.source_rule_id) Object.assign(row, { sourceRule: p.source_rule, sourceRuleId: p.source_rule_id });
      return row;
    });
}

// sourceRule and sourceRuleId name the calc rule that derived the value, when
// one did (#999).
export type VitalRow = { name: string; label: string; value: unknown; sampled: boolean; sourceRule?: string; sourceRuleId?: string };

// vitalRows is the same rule over the metric lane, with the live-series
// marker: a sampled row is the component speaking, an unsampled one is the
// contract's default standing in.
export function vitalRows(metrics: components["schemas"]["EffectiveMetricBody"][]): VitalRow[] {
  return (metrics ?? [])
    .filter((m) => m.value !== null && m.value !== undefined)
    .map((m) => {
      const row: VitalRow = { name: m.metric_type_name, label: entityLabel({ name: m.metric_type_name, label: m.label }), value: m.value, sampled: m.is_sampled };
      if (m.source_rule && m.source_rule_id) Object.assign(row, { sourceRule: m.source_rule, sourceRuleId: m.source_rule_id });
      return row;
    });
}

// leafAlarmSince: the component has no transitions read, so since-when comes
// from what took it down: the worst ACTIVE alarm and its age. Null when
// nothing is actively wrong (a cleared alarm is history, not a state).
export function leafAlarmSince(alarms: Alarm[], now: number, levels?: SeverityLevel[] | null): { message: string; severity: string; ts: string; ms: number } | null {
  const active = sortAlarms((alarms ?? []).filter((a) => a.active), levels);
  if (active.length === 0) return null;
  const worst = active[0];
  return { message: worst.message, severity: worst.severity, ts: worst.raised_at, ms: now - Date.parse(worst.raised_at) };
}

// dotVerdict reads the component's verdict off the fleet projection: any
// system's dot for it carries the component's OWN verdict, the same value
// wherever it appears.
export function dotVerdict(view: FleetView, componentId: string): string | null {
  for (const s of view.systems ?? []) {
    for (const d of s.dots ?? []) {
      if (d.component === componentId) return d.verdict ?? null;
    }
  }
  return null;
}
