import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The fleet view: the scoped projection every fleet surface reads (places,
// their systems, each system's components with its recorded verdict), and the
// small pure walks over its place tree that the outline (lib/outline) and the
// detail views (lib/detail) share. Verdicts are never computed here; the
// server records them and this module carries them.
//
// The band renderers this module once fed (bands, groupings, clusters, holes)
// retired with the band canvas (#861) and the location page that kept them
// (#872).

export type FleetView = components["schemas"]["FleetViewOutputBody"];
export type FleetLocation = NonNullable<FleetView["locations"]>[number];
export type FleetSystem = NonNullable<FleetView["systems"]>[number];

export const FLEET_VIEW_KEY = ["fleet-view"] as const;

export async function fleetView(): Promise<FleetView> {
  const { data, error } = await api.GET("/views/fleet");
  if (error) throw error;
  return data as FleetView;
}

// The index is memoized per view object: every path, crumb and landing rule
// reads it, and rebuilding a Map of the whole place tree per call turns a
// repaint into O(rows x locations) for no reason. A WeakMap keyed on the view
// keeps the memo exactly as long as the data it derives from.
const indexCache = new WeakMap<FleetView, Map<string, FleetLocation>>();

export function locationIndex(view: FleetView): Map<string, FleetLocation> {
  const hit = indexCache.get(view);
  if (hit) return hit;
  const index = new Map((view.locations ?? []).map((l) => [l.id, l]));
  indexCache.set(view, index);
  return index;
}

// childrenIndex inverts the parent pointers: whether a place has places
// beneath it.
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
