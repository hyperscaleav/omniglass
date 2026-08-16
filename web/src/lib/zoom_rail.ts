// The right rail's model (#630): one shape for every zoom, so the rail is one
// primitive consumed four times rather than four ad hoc panels. A headline
// (kicker, title, sub), a health ratio over the components in scope, a
// topology sentence with the location types present, a worst-first attention
// list, and a gaps footer. Pure: the ratio and the sort are decided here with
// no DOM, and every verdict word is the server's own.

import { ancestors, locationIndex, locationsWithoutSystems, type FleetLocation, type FleetSystem, type FleetView } from "./fleet";
import { entityLabel } from "./entities";
import { verdictOf, verdictRank, type Verdict } from "./health";
import type { ZoomLevel } from "./zoom";

export type RailScope = { zoom: ZoomLevel; locationId?: string | null; systemId?: string | null; componentId?: string | null };

export type RailRow = { key: string; label: string; verdict: Verdict | null; kind: "location" | "system" };

export type RailModel = {
  kicker: string;
  title: string;
  sub: string;
  ratio: { healthy: number; incomplete: number; degraded: number; outage: number; total: number };
  topology: string;
  types: string[];
  listTitle: string;
  list: RailRow[];
  gaps: string;
};

const worstFirst = (a: Verdict | null, b: Verdict | null) => (b ? verdictRank(b) : -1) - (a ? verdictRank(a) : -1);
const plural = (n: number, one: string, many: string) => (n === 1 ? one : many);

function ratioOf(systems: FleetSystem[]): RailModel["ratio"] {
  const seen = new Set<string>();
  const r = { healthy: 0, incomplete: 0, degraded: 0, outage: 0, total: 0 };
  for (const s of systems) {
    for (const d of s.dots ?? []) {
      // A shared component counts once, in whichever system lists it first.
      if (seen.has(d.component)) continue;
      seen.add(d.component);
      r.total++;
      const v = verdictOf(d.verdict);
      if (v) r[v]++;
    }
  }
  return r;
}

// depthIndex is one pass over the tree: each location's depth (root is 1)
// and its root, memoized per view. The rail reads depth for the topology
// sentence, the type ordering and the subtree test, and walking ancestors()
// per location for each of those is O(locations x depth) three times over on
// a page that re-renders on every zoom; one pass is O(locations).
const depthCache = new WeakMap<FleetView, Map<string, { depth: number; root: string; chain: string[] }>>();
function depthIndex(view: FleetView): Map<string, { depth: number; root: string; chain: string[] }> {
  const hit = depthCache.get(view);
  if (hit) return hit;
  const index = locationIndex(view);
  const out = new Map<string, { depth: number; root: string; chain: string[] }>();
  const resolve = (id: string, guard: number): { depth: number; root: string; chain: string[] } => {
    const done = out.get(id);
    if (done) return done;
    const l = index.get(id);
    if (!l) return { depth: 0, root: id, chain: [] };
    if (!l.parent || guard <= 0 || !index.has(l.parent)) {
      const r = { depth: 1, root: id, chain: [id] };
      out.set(id, r);
      return r;
    }
    const p = resolve(l.parent, guard - 1);
    const r = { depth: p.depth + 1, root: p.root, chain: [...p.chain, id] };
    out.set(id, r);
    return r;
  };
  for (const l of view.locations ?? []) resolve(l.id, index.size + 1);
  depthCache.set(view, out);
  return out;
}

function subtreeOf(rootId: string, view: FleetView): FleetLocation[] {
  const di = depthIndex(view);
  return (view.locations ?? []).filter((l) => di.get(l.id)?.chain.includes(rootId));
}

export function railModel(scope: RailScope, view: FleetView): RailModel {
  const index = locationIndex(view);
  const allSystems = view.systems ?? [];
  const holesAll = locationsWithoutSystems(view);
  // Types listed shallowest-first (the depth a type first appears at), so the
  // chips read as a ladder even though the model has none.
  const di = depthIndex(view);
  const distinctTypes = (locs: FleetLocation[]) => {
    // Tie on depth (a fleet with a campus root AND a building root, both at
    // depth 1) breaks toward the type that appears more often at that depth.
    const depth = new Map<string, number>();
    const countAtDepth = new Map<string, number>();
    for (const l of locs) {
      if (!l.location_type) continue;
      const d = di.get(l.id)?.depth ?? 0;
      const prev = depth.get(l.location_type);
      if (prev === undefined || d < prev) {
        depth.set(l.location_type, d);
        countAtDepth.set(l.location_type, 1);
      } else if (d === prev) {
        countAtDepth.set(l.location_type, (countAtDepth.get(l.location_type) ?? 0) + 1);
      }
    }
    return [...depth.entries()]
      .sort((a, b) => a[1] - b[1] || (countAtDepth.get(b[0]) ?? 0) - (countAtDepth.get(a[0]) ?? 0) || a[0].localeCompare(b[0]))
      .map(([t]) => t);
  };
  const gapsLine = (holes: number) => `${holes} ${plural(holes, "location has", "locations have")} no system`;

  if (scope.zoom === "location" && scope.locationId) {
    const here = index.get(scope.locationId);
    const subtree = subtreeOf(scope.locationId, view);
    const subtreeIds = new Set(subtree.map((l) => l.id));
    const systems = allSystems.filter((s) => s.location && subtreeIds.has(s.location));
    const chain = ancestors(scope.locationId, index);
    const ratio = ratioOf(systems);
    const kids = subtree.filter((l) => l.id !== scope.locationId);
    return {
      kicker: here?.location_type ?? "Location",
      title: here ? entityLabel(here) : "",
      sub: `${systems.length} ${plural(systems.length, "system", "systems")} · ${ratio.total} ${plural(ratio.total, "component", "components")}`,
      ratio,
      topology: `Path: ${chain.map((a) => a.location_type).join(" / ")}.`,
      types: distinctTypes(kids),
      listTitle: "Worst first",
      list: systems
        .map((s) => ({ key: s.id, label: entityLabel(s), verdict: verdictOf(s.verdict), kind: "system" as const }))
        .filter((r) => r.verdict && r.verdict !== "healthy")
        .sort((a, b) => worstFirst(a.verdict, b.verdict))
        .slice(0, 8),
      gaps: gapsLine(holesAll.filter((h) => subtreeIds.has(h.id)).length),
    };
  }

  if ((scope.zoom === "system" || scope.zoom === "component") && scope.systemId) {
    const sys = allSystems.find((s) => s.id === scope.systemId);
    const chain = sys?.location ? ancestors(sys.location, index) : [];
    const ratio = ratioOf(sys ? [sys] : []);
    return {
      kicker: scope.zoom === "system" ? "System" : "Component",
      title: sys ? entityLabel(sys) : "",
      sub: `${ratio.total} ${plural(ratio.total, "component", "components")}`,
      ratio,
      topology: chain.length ? `Placed on a ${chain[chain.length - 1].location_type}: ${chain.map((a) => entityLabel(a)).join(" / ")}.` : "",
      types: [],
      listTitle: "",
      list: [],
      gaps: "",
    };
  }

  // Fleet zoom (and any scope with nothing to anchor on).
  const roots = (view.locations ?? []).filter((l) => !l.parent);
  const leaves = (view.locations ?? []).filter((l) => !(view.locations ?? []).some((c) => c.parent === l.id));
  const depths = leaves.map((l) => di.get(l.id)?.depth ?? 0);
  const ratio = ratioOf(allSystems);
  const lo = depths.length ? Math.min(...depths) : 0;
  const hi = depths.length ? Math.max(...depths) : 0;
  return {
    kicker: "Fleet",
    title: "Your fleet",
    sub: `${allSystems.length} ${plural(allSystems.length, "system", "systems")} · ${ratio.total} ${plural(ratio.total, "component", "components")} · ${roots.length} ${plural(roots.length, "root", "roots")}`,
    ratio,
    topology: depths.length ? `Leaves sit ${lo === hi ? `${lo}` : `${lo} to ${hi}`} levels down. Nothing in the model requires a building or a floor.` : "",
    types: distinctTypes(view.locations ?? []),
    listTitle: "Roots needing attention",
    list: roots
      .map((r) => ({ key: r.id, label: entityLabel(r), verdict: verdictOf(r.verdict), kind: "location" as const }))
      .filter((r) => r.verdict && r.verdict !== "healthy")
      .sort((a, b) => worstFirst(a.verdict, b.verdict)),
    gaps: gapsLine(holesAll.length),
  };
}
