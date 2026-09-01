import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { verdictOf, verdictRank, worstVerdict, type Verdict } from "./health";
import { entityLabel } from "./entities";

// The fleet view model: the shapes the renderers draw, and the pure functions
// that derive them from the projection.
//
// This module is the seam the whole fleet surface is built on. Every renderer
// consumes the types below and NOTHING else: no role, no assignment, no quorum
// arithmetic, no API body. That is deliberate and it is load-bearing. The model
// underneath is still moving (typed slots landed in #647, plans and positions
// have not), and a renderer wired straight to the wire would be rewritten every
// time it shifts. Wired to these shapes, the mapper changes and the pixels do
// not.
//
// The other reason it exists: every derivation here is a pure function of its
// inputs, so the parts most likely to be quietly wrong (which dot is a ghost,
// which band a system lands in, what a location rolls up to) are unit-testable
// with no DOM, no server, and no fixtures beyond a literal.
//
// Verdicts are never computed here. The server records them and this module
// carries them; the one rollup below folds verdicts the server already decided,
// worst-wins, which is a different act from deciding one.

export type FleetView = components["schemas"]["FleetViewOutputBody"];
export type FleetLocation = NonNullable<FleetView["locations"]>[number];
export type FleetSystem = NonNullable<FleetView["systems"]>[number];
export type FleetDot = NonNullable<FleetSystem["dots"]>[number];

export const FLEET_VIEW_KEY = ["fleet-view"] as const;

export async function fleetView(): Promise<FleetView> {
  const { data, error } = await api.GET("/views/fleet");
  if (error) throw error;
  return data as FleetView;
}

// A Dot is one square. It carries the verdict that colours it, the two flags
// that decide whether it is drawn solid, ringed, or as a ghost outline, and the
// id a renderer navigates to.
export type Dot = {
  componentId: string;
  name: string;
  verdict: Verdict | null;
  // owned is the single cluster that draws this component solid. A shared
  // component is owned in exactly one place (its primary system) and a ghost
  // everywhere else, which is how a renderer shows every system depending on a
  // box without the fleet appearing to contain several of it.
  owned: boolean;
  shared: boolean;
};

// A SystemCluster is one system's dots, the unit a renderer wraps and the unit
// a click resolves to.
export type SystemCluster = {
  systemId: string;
  name: string;
  label: string;
  locationId: string | null;
  verdict: Verdict | null;
  dots: Dot[];
};

// A Band is one row of a band renderer: a label column and the clusters gathered
// under it. What "gathered under it" MEANS is the grouping function's business,
// not the band's, which is the point of the seam.
export type Band = {
  key: string;
  label: string;
  sublabel: string;
  verdict: Verdict | null;
  clusters: SystemCluster[];
  // systemCount and componentCount are the band's own arithmetic, and
  // componentCount counts a shared component ONCE across the whole band: it is
  // one physical box, and a band claiming otherwise is the double-count the
  // ghost rule exists to prevent.
  systemCount: number;
  componentCount: number;
  // recordedVerdict is the band subject's OWN server-recorded verdict, where
  // the grouping can name one (a location band can; a tag band cannot). The
  // chip renders this, never the fold above: the fold covers only in-scope
  // clusters, and a band disagreeing with its own detail page one click away
  // is a visible contradiction. Null when the grouping has no recorded row to
  // offer.
  recordedVerdict: Verdict | null;
  // depth is the band subject's own tree depth in levels (0 for a grouping
  // with no tree), the variable-depth fact the subtitle teaches.
  depth: number;
};

// A Grouping decides which band a system belongs to and how that band is
// labelled. Location ships; the seam is what lets standard, vendor or tag
// follow as a mapper rather than a second endpoint and a second renderer.
export type Grouping = {
  name: string;
  // bandFor returns the band key a system belongs to, or null to drop it.
  bandFor(system: FleetSystem, view: FleetView): string | null;
  // label and sublabel describe a band, given its key and the view.
  label(key: string, view: FleetView): string;
  sublabel(key: string, view: FleetView): string;
  // order sorts the band keys.
  order(keys: string[], view: FleetView): string[];
  // recordedVerdict names the band subject's own recorded verdict, where the
  // grouping has one to name; depth its subtree depth. Optional: a grouping
  // over a non-tree axis simply has neither fact.
  recordedVerdict?(key: string, view: FleetView): string | null | undefined;
  depth?(key: string, view: FleetView): number;
};

// The index is memoized per view object: bandsOf and every grouping method
// read it, and rebuilding a Map of the whole place tree once per system per
// band turns a repaint into O(systems x locations) for no reason. A
// WeakMap keyed on the view keeps the memo exactly as long as the data it
// derives from.
const indexCache = new WeakMap<FleetView, Map<string, FleetLocation>>();

export function locationIndex(view: FleetView): Map<string, FleetLocation> {
  const hit = indexCache.get(view);
  if (hit) return hit;
  const index = new Map((view.locations ?? []).map((l) => [l.id, l]));
  indexCache.set(view, index);
  return index;
}

// childrenIndex inverts the parent pointers: the walk subtreeDepth needs, and
// the one the later zooms will need to render a location's own children.
export function childrenIndex(view: FleetView): Map<string, FleetLocation[]> {
  const out = new Map<string, FleetLocation[]>();
  for (const l of view.locations ?? []) {
    if (!l.parent) continue;
    const list = out.get(l.parent);
    if (list) list.push(l);
    else out.set(l.parent, [l]);
  }
  return out;
}

// subtreeDepth measures how deep a root's tree goes, in levels including the
// root itself: the variable-depth fact the band subtitle exists to teach, rendered in
// the band's subtitle. Bounded like rootOf: a cycle in the data must not hang
// the band.
export function subtreeDepth(rootId: string, children: Map<string, FleetLocation[]>): number {
  let depth = 0;
  let frontier = [rootId];
  const seen = new Set<string>();
  while (frontier.length > 0) {
    depth++;
    const next: string[] = [];
    for (const id of frontier) {
      if (seen.has(id)) continue;
      seen.add(id);
      for (const child of children.get(id) ?? []) next.push(child.id);
    }
    frontier = next;
  }
  return depth;
}

// rootOf walks a location to its root. The walk is bounded by the number of
// locations, not by trust: a parent cycle in the data would otherwise hang the
// renderer, and the tree is arbitrary-depth by design so no fixed ladder can
// stand in for the walk.
export function rootOf(id: string | null | undefined, index: Map<string, FleetLocation>): FleetLocation | null {
  let current = id ? index.get(id) : undefined;
  let guard = index.size + 1;
  while (current && current.parent && guard-- > 0) {
    const next = index.get(current.parent);
    if (!next) break;
    current = next;
  }
  return current ?? null;
}

// ancestors returns root-first the chain down to id, which is what the
// breadcrumb and the type path read.
export function ancestors(id: string, index: Map<string, FleetLocation>): FleetLocation[] {
  const out: FleetLocation[] = [];
  let current = index.get(id);
  let guard = index.size + 1;
  while (current && guard-- > 0) {
    out.unshift(current);
    current = current.parent ? index.get(current.parent) : undefined;
  }
  return out;
}

// byRootLocation is the shipped grouping: one band per root location,
// geographical, which is the axis an operator arrives knowing.
export const byRootLocation: Grouping = {
  name: "location",
  bandFor(system, view) {
    return rootOf(system.location ?? null, locationIndex(view))?.id ?? null;
  },
  label(key, view) {
    const l = locationIndex(view).get(key);
    return l ? entityLabel(l) : key;
  },
  sublabel(key, view) {
    return locationIndex(view).get(key)?.location_type ?? "";
  },
  order(keys, view) {
    const index = locationIndex(view);
    return [...keys].sort((a, b) => (index.get(a) ? entityLabel(index.get(a)!) : a).localeCompare(index.get(b) ? entityLabel(index.get(b)!) : b));
  },
  recordedVerdict(key, view) {
    return locationIndex(view).get(key)?.verdict;
  },
  depth(key, view) {
    return subtreeDepth(key, childrenIndex(view));
  },
};

// byChildOfLocation is the location zoom's grouping (#635): one band per
// DIRECT child of the anchored location, whatever the child's type, plus a
// placed-here band (keyed by the anchor itself) for systems attached
// directly, ordered first. A factory rather than a constant because the
// anchor parameterizes it; the same bandsOf renders it, which is the seam
// the epic promised (grouping is a mapper, never a second renderer).
export function byChildOfLocation(locationId: string): Grouping {
  return {
    name: "children",
    bandFor(system, view) {
      if (!system.location) return null;
      if (system.location === locationId) return locationId;
      // Walk the system's chain root-first and take the step BELOW the
      // anchor: that step is the direct child whose band gathers everything
      // beneath it. A chain that never passes the anchor is outside this
      // zoom's subtree and is dropped.
      const chain = ancestors(system.location, locationIndex(view));
      const at = chain.findIndex((l) => l.id === locationId);
      if (at < 0 || at === chain.length - 1) return null;
      return chain[at + 1].id;
    },
    label(key, view) {
      const l = locationIndex(view).get(key);
      if (!l) return key;
      return key === locationId ? "Placed here" : entityLabel(l);
    },
    sublabel(key, view) {
      if (key === locationId) return "";
      return locationIndex(view).get(key)?.location_type ?? "";
    },
    order(keys, view) {
      const index = locationIndex(view);
      const label = (k: string) => (index.get(k) ? entityLabel(index.get(k)!) : k);
      return [...keys].sort((a, b) => {
        if (a === locationId) return -1;
        if (b === locationId) return 1;
        return label(a).localeCompare(label(b));
      });
    },
    recordedVerdict(key, view) {
      return locationIndex(view).get(key)?.verdict;
    },
    depth(key, view) {
      return subtreeDepth(key, childrenIndex(view));
    },
  };
}

// holesUnder scopes the holes to one location's subtree, grouped the way
// byChildOfLocation bands them: under the direct child that contains each, or
// under the anchor itself for a bare leaf directly beneath it.
export function holesUnder(locationId: string, view: FleetView): Map<string, FleetLocation[]> {
  const index = locationIndex(view);
  const out = new Map<string, FleetLocation[]>();
  for (const hole of locationsWithoutSystems(view)) {
    if (hole.id === locationId) continue;
    const chain = ancestors(hole.id, index);
    const at = chain.findIndex((l) => l.id === locationId);
    if (at < 0 || at === chain.length - 1) continue;
    // A hole directly under the anchor is the anchor's own gap; a deeper one
    // belongs to the child band that contains it.
    const bandKey = chain[at + 1].id === hole.id ? locationId : chain[at + 1].id;
    const list = out.get(bandKey);
    if (list) list.push(hole);
    else out.set(bandKey, [hole]);
  }
  return out;
}

export function toCluster(system: FleetSystem): SystemCluster {
  return {
    systemId: system.id,
    name: system.name,
    label: entityLabel(system),
    locationId: system.location || null,
    verdict: verdictOf(system.verdict),
    // The wire is a set (the scoped tree query carries no ORDER BY), so the
    // drawing order is decided here, deterministically: a renderer that
    // reshuffles its dots between refreshes reads as change where none
    // happened.
    dots: (system.dots ?? [])
      .map((d) => ({
        componentId: d.component,
        name: d.name,
        verdict: verdictOf(d.verdict),
        owned: d.primary,
        shared: d.shared,
      }))
      .sort((a, b) => a.name.localeCompare(b.name) || a.componentId.localeCompare(b.componentId)),
  };
}

// bandsOf is the whole gathering step, and the only place a band renderer learns how
// the fleet is arranged. Swap the grouping and the same dots land in different
// rows with no renderer touched.
export function bandsOf(view: FleetView, grouping: Grouping = byRootLocation): Band[] {
  const groups = new Map<string, FleetSystem[]>();
  for (const system of view.systems ?? []) {
    const key = grouping.bandFor(system, view);
    if (key === null) continue;
    const list = groups.get(key);
    if (list) list.push(system);
    else groups.set(key, [system]);
  }

  return grouping.order([...groups.keys()], view).map((key) => {
    const systems = groups.get(key) ?? [];
    // Clusters draw worst-first, then by label: the operator's eye lands on
    // what needs attention first, and the tie order is stable (the wire is a
    // set, and a renderer must not reshuffle between reads).
    const rank = (v: Verdict | null) => (v ? verdictRank(v) : -1);
    const clusters = systems
      .map(toCluster)
      .sort((a, b) => rank(b.verdict) - rank(a.verdict) || a.label.localeCompare(b.label) || a.name.localeCompare(b.name) || a.systemId.localeCompare(b.systemId));
    // A shared component appears in several clusters and is one box, so the
    // band counts distinct components rather than dots.
    const distinct = new Set<string>();
    for (const c of clusters) for (const d of c.dots) distinct.add(d.componentId);
    return {
      key,
      label: grouping.label(key, view),
      sublabel: grouping.sublabel(key, view),
      verdict: worstVerdict(clusters.map((c) => c.verdict)),
      clusters,
      systemCount: clusters.length,
      componentCount: distinct.size,
      recordedVerdict: verdictOf(grouping.recordedVerdict?.(key, view)),
      depth: grouping.depth?.(key, view) ?? 0,
    };
  });
}

// locationsWithoutSystems names the rooms that exist and hold nothing: the
// dashed holes a renderer draws. A hole is the half of the fleet a list view
// cannot show, and naming it is most of what the band face is for.
//
// It answers ONLY from what the view contains, which matters because the system
// tier is scoped independently of the place tree. A caller who may read
// locations but not systems receives a view with no systems in it, and "every
// leaf is a hole" would then be a confident lie about a fully commissioned
// fleet. There is no way to tell that case from a genuinely empty one from
// here, so this refuses to guess: with no systems in view at all, it claims no
// holes. A brand-new fleet loses a true-but-obvious answer; a scoped operator
// is not told their rooms are empty when they are not.
export function locationsWithoutSystems(view: FleetView): FleetLocation[] {
  const systems = view.systems ?? [];
  if (systems.length === 0) return [];
  const placed = new Set(systems.map((s) => s.location).filter(Boolean) as string[]);
  const hasChild = new Set((view.locations ?? []).map((l) => l.parent).filter(Boolean) as string[]);
  // A leaf with no system is a hole. A branch without one is not: its rooms are
  // where the systems belong, and drawing a hole at every level above them
  // would bury the real gaps in noise.
  return (view.locations ?? []).filter((l) => !hasChild.has(l.id) && !placed.has(l.id));
}

// holesByRoot folds the holes under the band that draws them, through the
// same root walk the bands use. Location-specific by construction: a hole is
// a place, and only the location grouping has places for keys.
export function holesByRoot(view: FleetView): Map<string, FleetLocation[]> {
  const index = locationIndex(view);
  const out = new Map<string, FleetLocation[]>();
  for (const hole of locationsWithoutSystems(view)) {
    const root = rootOf(hole.id, index);
    if (!root) continue;
    const list = out.get(root.id);
    if (list) list.push(hole);
    else out.set(root.id, [hole]);
  }
  return out;
}

// holeOnlyBands names the roots the system bands cannot: a root whose subtree
// holds ONLY holes. A site nobody has commissioned yet is exactly the fleet's
// mid-commissioning story, and a renderer that builds bands from systems alone
// would leave that site invisible, which is the opposite of naming the gap.
// Empty clusters; the recorded verdict and depth come from the location like
// any other band's.
export function holeOnlyBands(view: FleetView): Band[] {
  const banded = new Set(
    (view.systems ?? [])
      .map((s) => byRootLocation.bandFor(s, view))
      .filter((k): k is string => k !== null),
  );
  const index = locationIndex(view);
  const children = childrenIndex(view);
  const out: Band[] = [];
  for (const key of holesByRoot(view).keys()) {
    if (banded.has(key)) continue;
    const root = index.get(key);
    if (!root) continue;
    out.push({
      key,
      label: entityLabel(root),
      sublabel: root.location_type ?? "",
      verdict: null,
      clusters: [],
      systemCount: 0,
      componentCount: 0,
      recordedVerdict: verdictOf(root.verdict),
      depth: subtreeDepth(key, children),
    });
  }
  return out.sort((a, b) => a.label.localeCompare(b.label));
}

// fleetTotals is the inspector's headline, counting a shared component once.
export function fleetTotals(view: FleetView): { systems: number; components: number; roots: number } {
  const distinct = new Set<string>();
  for (const s of view.systems ?? []) for (const d of s.dots ?? []) distinct.add(d.component);
  return {
    systems: (view.systems ?? []).length,
    components: distinct.size,
    roots: (view.locations ?? []).filter((l) => !l.parent).length,
  };
}
