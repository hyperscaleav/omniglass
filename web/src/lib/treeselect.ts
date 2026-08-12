// The pure core behind TreeSelect: turn a flat parent_id list into the ordered,
// depth-tagged sequence a hierarchical <select> renders. Pre-order DFS so a node
// is immediately followed by its descendants; siblings sort by rank (a soft tier
// signal like a location_type rank) then label. Kept pure (no DOM, no I/O) so the
// ordering and nesting are unit-testable on their own.

export interface TreeNode {
  id: string;
  value: string;
  label: string;
  parentId?: string | null;
  // Optional sibling sort key; lower sorts first. Absent ranks sort as 0.
  rank?: number;
}

export interface FlatOption {
  value: string;
  label: string;
  depth: number;
}

// pathTo returns the labels from the root down to and including the given node,
// which is the placement CONTEXT a create form shows beside a name field: names
// are unique within a placement now, so an operator has to see which placement
// they are naming inside of.
//
// It walks by id (parentId), not by label, and it is bounded: a cycle a bad
// payload could describe stops the walk instead of hanging the console. An id
// the set does not contain is an empty path, which every caller renders as "the
// chosen one" rather than as "no placement".
export function pathTo(nodes: TreeNode[], id: string): string[] {
  const byId = new Map(nodes.map((n) => [n.id, n] as const));
  const out: string[] = [];
  const seen = new Set<string>();
  let cur = byId.get(id);
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    out.unshift(cur.label);
    cur = cur.parentId ? byId.get(cur.parentId) : undefined;
  }
  return out;
}

const bySibling = (a: TreeNode, b: TreeNode): number =>
  (a.rank ?? 0) - (b.rank ?? 0) || a.label.localeCompare(b.label);

// flattenTree returns the nodes in pre-order with their depth. A node whose
// parentId is absent or points outside the set is a root. excludeSubtreeOf drops
// that node and everything beneath it (so an edited node cannot be reparented
// under itself or a descendant).
export function flattenTree(nodes: TreeNode[], excludeSubtreeOf?: string): FlatOption[] {
  const ids = new Set(nodes.map((n) => n.id));
  const children = new Map<string | null, TreeNode[]>();
  for (const n of nodes) {
    const key = n.parentId && ids.has(n.parentId) ? n.parentId : null;
    (children.get(key) ?? children.set(key, []).get(key)!).push(n);
  }
  for (const list of children.values()) list.sort(bySibling);

  const out: FlatOption[] = [];
  const walk = (key: string | null, depth: number) => {
    for (const n of children.get(key) ?? []) {
      if (n.id === excludeSubtreeOf) continue;
      out.push({ value: n.value, label: n.label, depth });
      walk(n.id, depth + 1);
    }
  };
  walk(null, 0);
  return out;
}
