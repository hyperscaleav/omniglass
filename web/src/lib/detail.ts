import { ancestors, locationIndex, type FleetSystem, type FleetView } from "./fleet";
import { entityLabel } from "./entities";

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
