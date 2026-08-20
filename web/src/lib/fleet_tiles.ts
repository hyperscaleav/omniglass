// The fleet zoom's summary tiles and its system-grain marks (design option B,
// ruled 2026-08-18): the question this zoom answers is WHICH SYSTEMS NEED ME
// AND HOW MANY, so the mark is one round dot per system, coloured by the
// system's verdict, banded per root, worst first. Component dots move one zoom
// down where there is room to read them. The tiles carry what the rail
// carried, counted over systems at this zoom. Pure; no verdict is computed.

import { bandsOf, byRootLocation, holeOnlyBands, locationsWithoutSystems, type Band, type FleetView } from "./fleet";
import { verdictOf, verdictRank, type Verdict } from "./health";
import { entityLabel } from "./entities";

export type FleetTiles = {
  systems: number;
  components: number;
  roots: number;
  attention: { outage: number; degraded: number; incomplete: number; total: number };
  gaps: number;
  ratio: { healthy: number; incomplete: number; degraded: number; outage: number; total: number };
  depth: { min: number; max: number };
};

export function fleetTiles(view: FleetView): FleetTiles {
  const systems = view.systems ?? [];
  const distinct = new Set<string>();
  for (const s of systems) for (const d of s.dots ?? []) distinct.add(d.component);
  const ratio = { healthy: 0, incomplete: 0, degraded: 0, outage: 0, total: 0 };
  for (const s of systems) {
    const v = verdictOf(s.verdict);
    ratio.total++;
    if (v) ratio[v]++;
  }
  const locs = view.locations ?? [];
  const parents = new Set(locs.map((l) => l.parent).filter(Boolean) as string[]);
  const byId = new Map(locs.map((l) => [l.id, l]));
  const depthOf = (id: string): number => {
    let d = 0;
    let cur = byId.get(id);
    let guard = locs.length + 1;
    while (cur && guard-- > 0) {
      d++;
      cur = cur.parent ? byId.get(cur.parent) : undefined;
    }
    return d;
  };
  const leafDepths = locs.filter((l) => !parents.has(l.id)).map((l) => depthOf(l.id));
  return {
    systems: systems.length,
    components: distinct.size,
    roots: locs.filter((l) => !l.parent).length,
    attention: {
      outage: ratio.outage,
      degraded: ratio.degraded,
      incomplete: ratio.incomplete,
      total: ratio.outage + ratio.degraded + ratio.incomplete,
    },
    gaps: locationsWithoutSystems(view).length,
    ratio,
    depth: { min: leafDepths.length ? Math.min(...leafDepths) : 0, max: leafDepths.length ? Math.max(...leafDepths) : 0 },
  };
}

export type MarkFilter = { verdicts?: Set<Verdict> };

// systemMarks bands the fleet by root with ONE cluster of ONE dot per system.
// The dot's verdict is the system's, and its componentId slot carries the
// SYSTEM id, so the same canvas paints it and a click resolves to the system.
// Clusters order worst-first, then by label. A verdict filter drops systems
// but keeps every band, empty if need be, so a filtered-out root reads as
// filtered rather than missing.
export function systemMarks(view: FleetView, filter: MarkFilter = {}): Band[] {
  const rank = (v: Verdict | null) => (v ? verdictRank(v) : -1);
  const bands = [...bandsOf(view, byRootLocation), ...holeOnlyBands(view)].sort((a, b) => a.label.localeCompare(b.label));
  return bands.map((band) => {
    const clusters = band.clusters
      .filter((c) => !filter.verdicts || (c.verdict !== null && filter.verdicts.has(c.verdict)))
      .sort((a, b) => rank(b.verdict) - rank(a.verdict) || a.label.localeCompare(b.label))
      .map((c) => ({
        ...c,
        dots: [{ componentId: c.systemId, name: c.label, verdict: c.verdict, owned: true, shared: false }],
      }));
    return { ...band, clusters };
  });
}

// markLabel names a system dot for the hover title: the label and the room.
export function markLabel(band: Band, systemId: string, view: FleetView): string {
  const c = band.clusters.find((x) => x.systemId === systemId);
  if (!c) return "";
  const room = c.locationId ? (view.locations ?? []).find((l) => l.id === c.locationId) : undefined;
  return room ? `${c.label} · ${entityLabel(room)}` : c.label;
}
