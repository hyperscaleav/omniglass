import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { verdictOf, worstVerdict, type Verdict } from "./health";

// The estate view model: the shapes the canvas renders, and the pure functions
// that derive them from the projection.
//
// This module is the seam the whole estate surface is built on. Every renderer
// consumes the types below and NOTHING else: no role, no assignment, no quorum
// arithmetic, no API body. That is deliberate and it is load-bearing. The model
// underneath is still moving (typed slots landed in #647, plans and positions
// have not), and a canvas wired straight to the wire would be rewritten every
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

export type EstateView = components["schemas"]["EstateViewOutputBody"];
export type EstateLocation = NonNullable<EstateView["locations"]>[number];
export type EstateSystem = NonNullable<EstateView["systems"]>[number];
export type EstateDot = NonNullable<EstateSystem["dots"]>[number];

export const ESTATE_VIEW_KEY = ["estate-view"] as const;

export async function estateView(): Promise<EstateView> {
  const { data, error } = await api.GET("/views/estate");
  if (error) throw error;
  return data as EstateView;
}

// A Dot is one square. It carries the verdict that colours it, the two flags
// that decide whether it is drawn solid, ringed, or as a ghost outline, and the
// id the canvas navigates to.
export type Dot = {
  componentId: string;
  name: string;
  verdict: Verdict | null;
  // owned is the single cluster that draws this component solid. A shared
  // component is owned in exactly one place (its primary system) and a ghost
  // everywhere else, which is how the canvas shows every system depending on a
  // box without the estate appearing to contain several of it.
  owned: boolean;
  shared: boolean;
};

// A SystemCluster is one system's dots, the unit the canvas wraps and the unit
// a click resolves to.
export type SystemCluster = {
  systemId: string;
  name: string;
  label: string;
  locationId: string | null;
  verdict: Verdict | null;
  dots: Dot[];
};

// A Band is one row of the canvas: a label column and the clusters gathered
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
};

// A Grouping decides which band a system belongs to and how that band is
// labelled. Location ships; the seam is what lets standard, vendor or tag
// follow as a mapper rather than a second endpoint and a second canvas.
export type Grouping = {
  name: string;
  // bandFor returns the band key a system belongs to, or null to drop it.
  bandFor(system: EstateSystem, view: EstateView): string | null;
  // label and sublabel describe a band, given its key and the view.
  label(key: string, view: EstateView): string;
  sublabel(key: string, view: EstateView): string;
  // order sorts the band keys.
  order(keys: string[], view: EstateView): string[];
};

const labelOf = (e: { name: string; display_name?: string | null }) => e.display_name?.trim() || e.name;

export function locationIndex(view: EstateView): Map<string, EstateLocation> {
  return new Map((view.locations ?? []).map((l) => [l.id, l]));
}

// rootOf walks a location to its root. The walk is bounded by the number of
// locations, not by trust: a parent cycle in the data would otherwise hang the
// canvas, and the tree is arbitrary-depth by design so no fixed ladder can
// stand in for the walk.
export function rootOf(id: string | null | undefined, index: Map<string, EstateLocation>): EstateLocation | null {
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
export function ancestors(id: string, index: Map<string, EstateLocation>): EstateLocation[] {
  const out: EstateLocation[] = [];
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
    return l ? labelOf(l) : key;
  },
  sublabel(key, view) {
    return locationIndex(view).get(key)?.location_type ?? "";
  },
  order(keys, view) {
    const index = locationIndex(view);
    return [...keys].sort((a, b) => (index.get(a) ? labelOf(index.get(a)!) : a).localeCompare(index.get(b) ? labelOf(index.get(b)!) : b));
  },
};

export function toCluster(system: EstateSystem): SystemCluster {
  return {
    systemId: system.id,
    name: system.name,
    label: labelOf(system),
    locationId: system.location || null,
    verdict: verdictOf(system.verdict),
    dots: (system.dots ?? []).map((d) => ({
      componentId: d.component,
      name: d.name,
      verdict: verdictOf(d.verdict),
      owned: d.primary,
      shared: d.shared,
    })),
  };
}

// bandsOf is the whole gathering step, and the only place the canvas learns how
// the estate is arranged. Swap the grouping and the same dots land in different
// rows with no renderer touched.
export function bandsOf(view: EstateView, grouping: Grouping = byRootLocation): Band[] {
  const groups = new Map<string, EstateSystem[]>();
  for (const system of view.systems ?? []) {
    const key = grouping.bandFor(system, view);
    if (key === null) continue;
    const list = groups.get(key);
    if (list) list.push(system);
    else groups.set(key, [system]);
  }

  return grouping.order([...groups.keys()], view).map((key) => {
    const systems = groups.get(key) ?? [];
    const clusters = systems.map(toCluster);
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
    };
  });
}

// systemsWithoutDots names the rooms that exist and hold nothing: the dashed
// holes the canvas draws. A hole is the half of the estate a list view cannot
// show, and naming it is most of what the canvas is for.
export function locationsWithoutSystems(view: EstateView): EstateLocation[] {
  const placed = new Set((view.systems ?? []).map((s) => s.location).filter(Boolean) as string[]);
  const hasChild = new Set((view.locations ?? []).map((l) => l.parent).filter(Boolean) as string[]);
  // A leaf with no system is a hole. A branch without one is not: its rooms are
  // where the systems belong, and drawing a hole at every level above them
  // would bury the real gaps in noise.
  return (view.locations ?? []).filter((l) => !hasChild.has(l.id) && !placed.has(l.id));
}

// estateTotals is the inspector's headline, counting a shared component once.
export function estateTotals(view: EstateView): { systems: number; components: number; roots: number } {
  const distinct = new Set<string>();
  for (const s of view.systems ?? []) for (const d of s.dots ?? []) distinct.add(d.component);
  return {
    systems: (view.systems ?? []).length,
    components: distinct.size,
    roots: (view.locations ?? []).filter((l) => !l.parent).length,
  };
}
