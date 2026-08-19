// The component leaf's derivations (#637): the membership rows the leaf lists
// and the collection card's state. Pure, so the one distinction the card
// exists to make is pinned with no DOM: a healthy node with a stale sample
// means the device or the path, not collection, and that sentence is why
// nodes need no zoom of their own.

import { locationIndex, type FleetView } from "./fleet";
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
