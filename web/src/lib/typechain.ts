// The one client-side reading of the console's inheriting type registries
// (component_type and system_type): a fact set on a node wins, an unset one
// comes from the nearest ancestor that has it, and nothing set anywhere is an
// absence rather than a guess.
//
// It is the client half of the server's `resolveTypeFacts` /
// `resolveSystemTypeFacts` walk (ADR-0095's first-non-null-wins rule). The walk
// was written out twice before this, once per registry, in
// `resolveComponentTypeIcon` and `resolveSystemTypeIcon`, each carrying its own
// copy of the depth bound; both now consume this, and so does the generated-name
// preview (lib/namegen.ts), which is the third fact to need it and the first
// where a wrong answer is about an address rather than a glyph.

// InheritedNode is the shape both registries share for the purposes of the walk:
// a name to be indexed by and the PARENT'S NAME to climb to. The fact itself is
// not part of the shape, because a caller picks it (see resolveInherited's
// `pick`), which is what lets one walk serve a stem, an icon, and an abbrev.
export interface InheritedNode {
  name: string;
  parent?: string;
}

// The same bound the server's walks use (internal/storage/component_types.go):
// it defends against a cycle a client sent us, and is never expected to be hit
// by real data.
const MAX_DEPTH = 32;

// resolveInherited returns the nearest set value of one fact, starting at the
// named type and climbing its parents.
//
// An EMPTY STRING counts as absent, not as a value. That is the server's rule
// rather than a convenience: the wire carries these facts with `omitempty`, so a
// type that inherits and a type that stores "" are indistinguishable by the time
// the console sees them, and treating "" as a set value would stop the walk at a
// node that was inheriting.
export function resolveInherited<T extends InheritedNode>(
  typeName: string | undefined,
  byName: Map<string, T>,
  pick: (t: T) => string | undefined,
): string | undefined {
  let cur = typeName ? byName.get(typeName) : undefined;
  for (let depth = 0; cur && depth < MAX_DEPTH; depth++) {
    const v = pick(cur);
    if (v) return v;
    cur = cur.parent ? byName.get(cur.parent) : undefined;
  }
  return undefined;
}
