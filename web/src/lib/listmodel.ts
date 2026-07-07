// The pure list-model behind the TreeList shell: building the flattened index
// from a forest, ancestor paths, the flatten/tree row sets, and parsing the
// client preferences (column order, widget board). Kept free of Solid and the DOM
// so the genuinely tricky derivations are unit-tested without rendering. TreeList
// is the thin reactive wrapper that feeds these from signals.
import { buildPredicate, type Chip, type FilterKey } from "./predicate";

// The minimal node shape these functions need: an id, a display label, and a
// forest of the same shape. Pages pass their own richer node type.
export type TreeLike<N> = { id: string; display: string; children: N[] };

export type Crumb = { id: string; display: string };
export type Row<N> = { n: N; depth: number; path: Crumb[] | null };
export type SortState = { key: string; dir: 1 | -1 } | null;
export type ListIndex<N> = {
  byId: Map<string, N>;
  parentOf: Map<string, N>;
  all: N[];
  containerIds: Set<string>;
};

// buildIndex flattens the forest depth-first: id -> node, child -> parent, the
// in-order node list (also the default flat order), and the ids that have
// children (for expand/collapse-all).
export function buildIndex<N extends TreeLike<N>>(roots: N[]): ListIndex<N> {
  const byId = new Map<string, N>();
  const parentOf = new Map<string, N>();
  const all: N[] = [];
  const containerIds = new Set<string>();
  const walk = (list: N[], parent: N | null) => {
    for (const n of list) {
      byId.set(n.id, n);
      all.push(n);
      if (parent) parentOf.set(n.id, parent);
      if (n.children.length) {
        containerIds.add(n.id);
        walk(n.children, n);
      }
    }
  };
  walk(roots, null);
  return { byId, parentOf, all, containerIds };
}

// pathOf returns a node's ancestors, root first (the breadcrumb).
export function pathOf<N extends TreeLike<N>>(index: ListIndex<N>, node: N): Crumb[] {
  const out: Crumb[] = [];
  let p = index.parentOf.get(node.id);
  while (p) {
    out.unshift({ id: p.id, display: p.display });
    p = index.parentOf.get(p.id);
  }
  return out;
}

// flattenRows filters the whole node set, then keeps index order (the tree walked
// depth-first, nesting preserved) by default, or applies the chosen column sort.
// Each row carries its ancestor path.
export function flattenRows<N extends TreeLike<N>>(
  index: ListIndex<N>,
  filterKeys: FilterKey<N>[],
  chips: Chip[],
  sort: SortState,
  sortVal: (n: N, key: string) => string | number,
): Row<N>[] {
  const pred = buildPredicate(filterKeys, chips);
  const list = index.all.filter(pred);
  if (sort) {
    const s = sort;
    list.sort((a, b) => {
      const x = sortVal(a, s.key);
      const y = sortVal(b, s.key);
      const r = typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y));
      return r * s.dir;
    });
  }
  return list.map((n) => ({ n, depth: 0, path: pathOf(index, n) }));
}

// treeRows walks the forest, descending only into expanded containers.
export function treeRows<N extends TreeLike<N>>(roots: N[], expanded: Set<string>): Row<N>[] {
  const out: Row<N>[] = [];
  const walk = (list: N[], depth: number) => {
    for (const n of list) {
      out.push({ n, depth, path: null });
      if (n.children.length && expanded.has(n.id)) walk(n.children, depth + 1);
    }
  };
  walk(roots, 0);
  return out;
}

// parsePref reads a stored client preference (column order, widget board): keep
// only valid keys, de-dupe, preserve first-seen order. Returns null when absent or
// unusable so the caller falls back to its default; an explicit empty array is
// honored (the operator hid everything).
export function parsePref(raw: string | null, valid: string[]): string[] | null {
  if (!raw) return null;
  try {
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return null;
    const clean = [...new Set(arr.filter((k) => valid.includes(k)))];
    return clean.length || arr.length === 0 ? clean : null;
  } catch {
    return null;
  }
}

// toggleItem adds or removes a key, appending at the end (membership ops).
export function toggleItem(list: string[], item: string): string[] {
  return list.includes(item) ? list.filter((x) => x !== item) : [...list, item];
}

// moveItem reorders an element from one position to another (drag reorder).
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  const a = [...list];
  const [x] = a.splice(from, 1);
  a.splice(to, 0, x);
  return a;
}

// allExpanded reports whether every container is currently expanded.
export function allExpanded(containerIds: Set<string>, expanded: Set<string>): boolean {
  return containerIds.size > 0 && [...containerIds].every((id) => expanded.has(id));
}
