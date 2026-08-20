// The component leaf's derivations (#637): the membership rows the leaf lists
// and the collection card's state. Pure, so the one distinction the card
// exists to make is pinned with no DOM: a healthy node with a stale sample
// means the device or the path, not collection, and that sentence is why
// nodes need no zoom of their own.

import { locationIndex, type FleetView } from "./fleet";
import { sortAlarms, type Alarm } from "./alarms";
import type { components } from "../api/schema.gen";
import type { Member } from "./members";
import type { Node } from "./nodes";
import { nodeStatus } from "./nodes";
import type { ReachInterface } from "./reachability";
import { verdictWord } from "./reachability";
import { entityLabel } from "./entities";

export type CollectionState = {
  // collecting: fresh sample, node live. device-or-path: the sample went
  // stale while the node stayed live, so collection is fine and the device or
  // the route to it is not. node-offline: the node itself stopped
  // heartbeating, so staleness says nothing about the device. down: the
  // device answered, badly, which is a device fact and not a collection one.
  kind: "collecting" | "device-or-path" | "node-offline" | "down" | "unknown";
  node?: string;
};

export function collectionState(iface: ReachInterface, node: Node | undefined, now: number = Date.now()): CollectionState {
  const nodeDown = !node || nodeStatus(node, now) !== "up";
  const word = verdictWord(iface.verdict ?? null, now);
  if (word === "unknown") return { kind: nodeDown ? "node-offline" : "unknown", node: iface.node };
  if (word === "stale") return { kind: nodeDown ? "node-offline" : "device-or-path", node: iface.node };
  if (word === "down") return { kind: "down", node: iface.node };
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

export type IdentityRow = { name: string; label: string; value: unknown };

// identityRows keeps every property that resolves to a value (an explicit
// set or the contract default): model, serial, firmware are the point, but
// the rule is the value's presence, not a hand-kept key list.
export function identityRows(props: components["schemas"]["EffectivePropertyBody"][]): IdentityRow[] {
  return (props ?? [])
    .filter((p) => p.value !== null && p.value !== undefined)
    .map((p) => ({ name: p.property_type_name, label: entityLabel({ name: p.property_type_name, label: p.label }), value: p.value }));
}

export type VitalRow = { name: string; label: string; value: unknown; sampled: boolean };

// vitalRows is the same rule over the metric lane, with the live-series
// marker: a sampled row is the device speaking, an unsampled one is the
// contract's default standing in.
export function vitalRows(metrics: components["schemas"]["EffectiveMetricBody"][]): VitalRow[] {
  return (metrics ?? [])
    .filter((m) => m.value !== null && m.value !== undefined)
    .map((m) => ({ name: m.metric_type_name, label: entityLabel({ name: m.metric_type_name, label: m.label }), value: m.value, sampled: m.is_sampled }));
}

// leafAlarmSince: the component has no transitions read, so since-when comes
// from what took it down: the worst ACTIVE alarm and its age. Null when
// nothing is actively wrong (a cleared alarm is history, not a state).
export function leafAlarmSince(alarms: Alarm[], now: number): { message: string; severity: string; ts: string; ms: number } | null {
  const active = sortAlarms((alarms ?? []).filter((a) => a.active));
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
