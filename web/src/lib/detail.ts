import { ancestors, childrenIndex, locationIndex, type FleetSystem, type FleetView } from "./fleet";
import { countWord } from "./outline";
import { entityLabel } from "./entities";
import type { Verdict } from "./health";
import type { SystemBody, ComponentCard } from "./system_zoom";

// The detail view's model (#872). Systems are the unit Omniglass monitors;
// places are folders and metadata about where; components are the pieces of
// systems. So there is one detail view per system, its place shown as a card
// of context, and a place holding exactly one system lands on that system
// rather than showing a second, thinner view of the same room.
//
// Pure: where an address lands and what the path says are the facts an
// operator would dispute, so they are tested here without a DOM.

export type Landing = { kind: "system"; id: string } | { kind: "place" };

export type Crumb = { kind: "location" | "system"; id: string; label: string };

// The systems bound at a place, by label.
export function systemsAtPlace(view: FleetView, placeId: string): FleetSystem[] {
  return (view.systems ?? [])
    .filter((s) => s.location === placeId)
    .sort((a, b) => entityLabel(a).localeCompare(entityLabel(b)) || a.id.localeCompare(b.id));
}

// Where a place's address lands: on its system when it holds exactly one,
// since that system is what the operator came to see; otherwise on the place.
export function landingFor(view: FleetView, placeId: string): Landing {
  const here = systemsAtPlace(view, placeId);
  return here.length === 1 ? { kind: "system", id: here[0].id } : { kind: "place" };
}

const placeCrumbs = (view: FleetView, placeId: string): Crumb[] =>
  ancestors(placeId, locationIndex(view)).map((l) => ({ kind: "location" as const, id: l.id, label: entityLabel(l) }));

// A system's path. A sole system's view is titled by its place, so the path
// ends at the place's parent; a place shared by several systems has a view of
// its own, so the path keeps it. An unplaced system has no path.
export function systemCrumbs(view: FleetView, systemId: string): Crumb[] {
  const s = (view.systems ?? []).find((x) => x.id === systemId);
  if (!s?.location) return [];
  const chain = placeCrumbs(view, s.location);
  return systemsAtPlace(view, s.location).length === 1 ? chain.slice(0, -1) : chain;
}

// A component's path: the places down to where it sits, then its primary
// system. Where that system is the only one at the component's own place, the
// place and the system are one crumb (the system, whose view is the room's),
// so the room is never named twice.
export function componentCrumbs(view: FleetView, locationId: string | null | undefined, primarySystemId: string | null | undefined): Crumb[] {
  const chain = locationId ? placeCrumbs(view, locationId) : [];
  const s = primarySystemId ? (view.systems ?? []).find((x) => x.id === primarySystemId) : undefined;
  if (!s) return chain;
  const crumb: Crumb = { kind: "system", id: s.id, label: entityLabel(s) };
  if (s.location && s.location === locationId && systemsAtPlace(view, s.location).length === 1) return [...chain.slice(0, -1), crumb];
  return [...chain, crumb];
}

// The roles a component staffs in one system, by label: read from the
// system's declared roles, since a membership says THAT a component belongs,
// and the role says what it does there.
export function rolesOf(componentName: string, declared: { name: string; label?: string; assigned_to?: string[] | null }[]): string[] {
  return declared.filter((r) => (r.assigned_to ?? []).includes(componentName)).map((r) => entityLabel({ name: r.name, label: r.label }));
}

// A system's components as rows (#872), the outline's row idiom rather than a
// grid of cards. A member reads its role in a column; a role earns a group
// (a header with its arithmetic, its occupants beneath, its gap as empty
// slots) only where it says something a column cannot: a quorum beyond one,
// a shortfall, or nobody staffing it. An unstaffed role is a commissioning
// gap and reads incomplete; a short staffed one reads its impact.

export type MemberRow = {
  id: string;
  name: string;
  label: string;
  role: string;
  product: string;
  icon: string;
  health: Verdict | null;
  alarm?: string;
  also: string[];
  noRole: boolean;
};
export type MemberGroup = {
  key: string;
  label: string;
  arithmetic: string;
  tone: "incomplete" | "degraded" | "outage" | null;
  members: MemberRow[];
  empty: number;
};
export type MemberContext = {
  verdict: (id: string) => Verdict | null;
  alarm: (id: string) => string | undefined;
  product: (id: string) => string;
  label: (id: string, name: string) => string;
  icon: (id: string) => string;
};

export function memberModel(body: SystemBody, ctx: MemberContext): { rows: MemberRow[]; groups: MemberGroup[] } {
  const row = (c: ComponentCard, role: string): MemberRow => ({
    id: c.componentId,
    name: c.name,
    label: ctx.label(c.componentId, c.name),
    role,
    product: ctx.product(c.componentId),
    icon: ctx.icon(c.componentId),
    health: ctx.verdict(c.componentId),
    alarm: ctx.alarm(c.componentId),
    also: c.shared,
    noRole: c.noRole && c.roles.length === 0,
  });
  const rows = body.cards.map((c) => row(c, c.roles.map((r) => (r.position ? `${r.label} (${r.position})` : r.label)).join(", ")));
  const groups = body.groups.map((g) => {
    const tone: MemberGroup["tone"] = g.short === 0 ? null : g.members.length === 0 ? "incomplete" : g.impact === "outage" ? "outage" : "degraded";
    return {
      key: g.name,
      label: g.label,
      arithmetic: `${g.satisfying} of ${g.quorum}${g.spare > 0 ? `, ${g.spare} spare` : ""}`,
      tone,
      // Inside its group a member's role is the group; the column names its
      // position, if any (what tells it from its siblings),
      // and the other roles it fills, since its one home is this group.
      members: g.memberCards.map((c) => row(c, [
        c.roles.find((r) => r.label === g.label)?.position ?? "",
        ...c.roles.filter((r) => r.label !== g.label).map((r) => (r.position ? `${r.label} (${r.position})` : r.label)),
      ].filter(Boolean).join(", "))),
      // Seats nobody staffs: a down occupant still holds its seat, and its
      // failure is the alarms' to explain, not a gap.
      empty: Math.max(0, g.quorum - g.members.length),
    };
  });
  return { rows, groups };
}

// What a place holds, in the outline's words: the places directly beneath it
// counted by their type's registry label ("2 buildings, 1 floor"), or Empty.
export function contentsOf(view: FleetView, placeId: string, types: { name: string; label?: string }[]): string {
  const counts = new Map<string, number>();
  for (const c of childrenIndex(view).get(placeId) ?? []) counts.set(c.location_type, (counts.get(c.location_type) ?? 0) + 1);
  const label = (t: string) => entityLabel(types.find((x) => x.name === t) ?? { name: t });
  const parts = [...counts.entries()].map(([t, n]) => countWord(label(t), n));
  return parts.length > 0 ? parts.join(", ") : "Empty";
}
