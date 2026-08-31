import { ancestors, childrenIndex, locationIndex, type FleetLocation, type FleetSystem, type FleetView } from "./fleet";
import { entityLabel } from "./entities";

// The explorer's pure core (#826): the drill-down tree as functions of the
// fleet view, no DOM, so the page is a thin renderer and the rules are
// unit-testable. The tree assumes nothing about depth or types: the first
// column is the locations with no parent, whatever their types, and every
// later column is one node's children, locations of any type first, systems
// after. A location with one system and no sub-locations collapses into that
// system's row (the system stands in for its room), and a location's verdict
// rolls up from every system under it, at any depth.

export type Verdict = string;
const RANK: Record<string, number> = { outage: 0, degraded: 1, incomplete: 2, healthy: 3 };
const rank = (v: string | null | undefined) => (v && v in RANK ? RANK[v] : 9);

export type ExploreHit = { kind: "system" | "location"; id: string; label: string; path: string; verdict: Verdict | null };

const byLabel = <T extends { label?: string; name: string }>(a: T, b: T) => entityLabel(a).localeCompare(entityLabel(b));

function systemsIn(view: FleetView, locationId: string): FleetSystem[] {
  return (view.systems ?? []).filter((s) => s.location === locationId).sort(byLabel);
}

// Every system under a location, at any depth. Bounded by a seen-set so a
// parent cycle in the data cannot hang the walk.
export function systemsUnder(view: FleetView, locationId: string): FleetSystem[] {
  const children = childrenIndex(view);
  const out: FleetSystem[] = [];
  const seen = new Set<string>();
  const stack = [locationId];
  while (stack.length) {
    const id = stack.pop()!;
    if (seen.has(id)) continue;
    seen.add(id);
    out.push(...systemsIn(view, id));
    for (const c of children.get(id) ?? []) stack.push(c.id);
  }
  return out;
}

export function subtreeVerdict(view: FleetView, locationId: string): Verdict | null {
  const all = systemsUnder(view, locationId);
  if (all.length === 0) return null;
  return all.map((s) => s.verdict ?? null).sort((a, b) => rank(a) - rank(b))[0] ?? null;
}

function displaces(view: FleetView, loc: FleetLocation): FleetSystem | null {
  const kids = childrenIndex(view).get(loc.id) ?? [];
  if (kids.length > 0) return null;
  const here = systemsIn(view, loc.id);
  return here.length === 1 ? here[0] : null;
}

export function pathLabel(view: FleetView, locationId: string): string {
  return ancestors(locationId, locationIndex(view)).map((l) => entityLabel(l)).join(" / ");
}

// Where a ?node= address lands: the path to open and the row to select. A
// system's path is its location's ancestry, minus the location itself when
// the location collapses into the system's row.
export function pathForNode(view: FleetView, node: string): { path: string[]; selected: string } | null {
  const index = locationIndex(view);
  // A name-shaped address resolves when it names exactly one system or, failing
  // that, exactly one location (#759's rule, the uuid always winning). Names
  // repeat across placements, so an ambiguous one resolves to nothing rather
  // than guessing.
  let nodeId = node;
  if (!index.has(node) && !(view.systems ?? []).some((s) => s.id === node)) {
    const systems = (view.systems ?? []).filter((s) => s.name === node);
    const locations = (view.locations ?? []).filter((l) => l.name === node);
    if (systems.length === 1) nodeId = systems[0].id;
    else if (systems.length === 0 && locations.length === 1) nodeId = locations[0].id;
    else return null;
  }
  const loc = index.get(nodeId);
  if (loc) return { path: ancestors(nodeId, index).map((l) => l.id), selected: nodeId };
  const sys = (view.systems ?? []).find((s) => s.id === nodeId);
  if (!sys || !sys.location) return null;
  const chain = ancestors(sys.location, index).map((l) => l.id);
  const here = index.get(sys.location);
  if (here && displaces(view, here)) chain.pop();
  return { path: chain, selected: nodeId };
}

// Search by label, name, or path fragment: systems first (worst first, then
// label), then locations by label. The path rides on every hit because a
// name is unique only within its placement.
export function searchTree(view: FleetView, query: string): ExploreHit[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const hits: ExploreHit[] = [];
  for (const s of view.systems ?? []) {
    const path = s.location ? pathLabel(view, s.location) : "";
    const text = `${entityLabel(s)} ${s.name} ${path}`.toLowerCase();
    if (text.includes(q)) hits.push({ kind: "system", id: s.id, label: entityLabel(s), path, verdict: s.verdict ?? null });
  }
  hits.sort((a, b) => rank(a.verdict) - rank(b.verdict) || a.label.localeCompare(b.label));
  const locs: ExploreHit[] = [];
  for (const l of view.locations ?? []) {
    const path = pathLabel(view, l.id);
    const text = `${entityLabel(l)} ${l.name} ${path}`.toLowerCase();
    if (text.includes(q)) locs.push({ kind: "location", id: l.id, label: entityLabel(l), path, verdict: subtreeVerdict(view, l.id) });
  }
  locs.sort((a, b) => a.label.localeCompare(b.label));
  return [...hits, ...locs];
}
