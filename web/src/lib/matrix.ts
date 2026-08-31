import { type FleetLocation, type FleetSystem, type FleetView } from "./fleet";
import { entityLabel } from "./entities";
import { cutNodesFor, cutTypeFor } from "./place_cut";
import { countsOf, emptyCounts, type Counts, type ExploreOptions } from "./explore_view";
import { verdictOf } from "./health";

// The matrix's pure core (#840): place against standard.
//
// This is the one view that answers a question no other renderer can, which is
// how one standard is doing everywhere at once. Every other face groups by
// place and shows health inside it; this pivots so a column is a standard and
// a reader can scan down it.
//
// Rows follow the SAME per-root cut the cards use, so the two faces agree
// about what a unit of the fleet is. An earlier sketch assumed rows were
// "sites then floors", which is the depth assumption the whole epic exists to
// remove.
//
// The standard is not on the fleet wire. It is on the systems list, which the
// table face already loads, so the caller passes a lookup rather than this
// module learning about a second query. That keeps the pivot pure and makes
// the join visible at the call site instead of hidden here.

// A system that conforms to no standard is first class (see lib/systems.ts),
// so it gets a column rather than being dropped.
export const NO_STANDARD = "— none";

export type MatrixCell = { count: number; counts: Counts };
export type MatrixRow = { id: string; label: string; type: string; indent: boolean; cells: Record<string, MatrixCell> };
export type MatrixModel = {
  columns: string[];
  rows: MatrixRow[];
  // Past a size, a cell of dots stops being readable and the table becomes a
  // report of counts. The model says which it is so the renderer does not have
  // to decide, and so the threshold is testable.
  dense: boolean;
};

export const DENSE_ABOVE = 120;

function systemsUnder(view: FleetView, nodeId: string, children: Map<string, FleetLocation[]>): FleetSystem[] {
  const out: FleetSystem[] = [];
  const seen = new Set<string>();
  let frontier = [nodeId];
  let guard = (view.locations?.length ?? 0) + 1;
  const systems = view.systems ?? [];
  while (frontier.length > 0 && guard-- > 0) {
    const next: string[] = [];
    for (const id of frontier) {
      if (seen.has(id)) continue;
      seen.add(id);
      for (const s of systems) if (s.location === id) out.push(s);
      for (const c of children.get(id) ?? []) next.push(c.id);
    }
    frontier = next;
  }
  return out;
}

function childrenOf(view: FleetView): Map<string, FleetLocation[]> {
  const out = new Map<string, FleetLocation[]>();
  for (const l of view.locations ?? []) {
    if (!l.parent) continue;
    const list = out.get(l.parent);
    if (list) list.push(l);
    else out.set(l.parent, [l]);
  }
  return out;
}

function cellFor(systems: FleetSystem[]): MatrixCell {
  return {
    count: systems.length,
    counts: countsOf(systems.map((s) => ({ verdict: verdictOf(s.verdict) }))),
  };
}

function keep(s: FleetSystem, opts: ExploreOptions): boolean {
  if (!opts.attentionOnly) return true;
  const v = verdictOf(s.verdict);
  return v === "degraded" || v === "outage";
}

// matrixFor pivots the fleet: one row per root and, while the table is small
// enough to read, one indented row per cut node under it. Columns are the
// standards actually present, so a fleet of one standard is one column
// rather than a catalogue of empties.
export function matrixFor(
  view: FleetView,
  standardOf: (systemId: string) => string | undefined,
  opts: ExploreOptions,
): MatrixModel {
  const children = childrenOf(view);
  const roots = (view.locations ?? []).filter((l) => !l.parent);
  const all = (view.systems ?? []).filter((s) => keep(s, opts));
  const dense = all.length > DENSE_ABOVE;

  const columnSet = new Set<string>();
  for (const s of all) columnSet.add(standardOf(s.id) || NO_STANDARD);
  const columns = [...columnSet].sort((a, b) =>
    a === NO_STANDARD ? 1 : b === NO_STANDARD ? -1 : a.localeCompare(b),
  );

  const rowFor = (node: FleetLocation, indent: boolean): MatrixRow | null => {
    const mine = systemsUnder(view, node.id, children).filter((s) => keep(s, opts));
    if (mine.length === 0) return null;
    const cells: Record<string, MatrixCell> = {};
    for (const col of columns) {
      const hit = mine.filter((s) => (standardOf(s.id) || NO_STANDARD) === col);
      if (hit.length > 0) cells[col] = cellFor(hit);
    }
    return { id: node.id, label: entityLabel(node), type: node.location_type, indent, cells };
  };

  const rows: MatrixRow[] = [];
  for (const root of roots.sort((a, b) => entityLabel(a).localeCompare(entityLabel(b)))) {
    const head = rowFor(root, false);
    if (!head) continue;
    rows.push(head);
    // Only while the table can still be read as a grid rather than a report.
    if (dense) continue;
    for (const node of cutNodesFor(view, root.id)) {
      if (node.id === root.id) continue;
      const sub = rowFor(node, true);
      if (sub) rows.push(sub);
    }
  }

  return { columns, rows, dense };
}

// The cut type each root uses, for the row group's caption. Exposed so the
// renderer can say "3 buildings" rather than inventing a word for the level.
export function cutLabelFor(view: FleetView, rootId: string): string {
  return cutTypeFor(view, rootId);
}

export const emptyCell = (): MatrixCell => ({ count: 0, counts: emptyCounts() });
