import { ROOT_PLACEMENT } from "./location_types";

// The location type hierarchy, read off the registry (ADR-0102).
//
// A location type is customer data: one fleet runs campus, building, floor,
// room; another runs plot, sector, coordinate. So nothing in the console may
// decide a type's place in the hierarchy from its NAME. The registry already
// says where each type may sit (allowed_parent_types), and that is the only
// fact these read. Pure, so the answers are testable without a server.

type TypeShape = { name: string; allowed_parent_types: string[] };

// An empty allowed_parent_types is unconstrained: the type may sit anywhere,
// the top included.
const unconstrained = (t: TypeShape) => t.allowed_parent_types.length === 0;

// typeRanks orders the hierarchy: a type ranks one below the deepest type it
// may sit under, and a type that may sit at the root (or anywhere) with no
// other parent ranks 0. So in the shipped registry campus, building, floor and
// room rank 0, 1, 2, 3 even though a building may also sit at the root, and a
// customer's plot, sector, coordinate rank the same way. It is what the tree
// sorts a Type column by, weights a name by, and orders a parent picker by.
//
// Only types a placement can actually reach from the top are ranked; a type
// whose only parents form a cycle is left out rather than guessed at, and a
// caller sorts it last. A cycle among reachable types is cut where it closes.
export function typeRanks(types: TypeShape[]): Map<string, number> {
  const byName = new Map(types.map((t) => [t.name, t] as const));
  const atTop = (t: TypeShape) => unconstrained(t) || t.allowed_parent_types.includes(ROOT_PLACEMENT);

  // Reachable from the top: a type at the top, or one that may sit under a
  // reachable type.
  const reachable = new Set(types.filter(atTop).map((t) => t.name));
  for (let changed = true; changed; ) {
    changed = false;
    for (const t of types) {
      if (reachable.has(t.name)) continue;
      if (t.allowed_parent_types.some((p) => reachable.has(p))) {
        reachable.add(t.name);
        changed = true;
      }
    }
  }

  const rank = new Map<string, number>();
  const visiting = new Set<string>();
  const rankOf = (name: string): number => {
    const known = rank.get(name);
    if (known !== undefined) return known;
    visiting.add(name);
    let r = 0;
    for (const p of byName.get(name)?.allowed_parent_types ?? []) {
      if (p === ROOT_PLACEMENT || !reachable.has(p) || visiting.has(p)) continue;
      r = Math.max(r, rankOf(p) + 1);
    }
    visiting.delete(name);
    rank.set(name, r);
    return r;
  };
  for (const name of reachable) rankOf(name);
  return rank;
}

// canHoldChildren is whether any location may be placed under a location of
// this type: true when some type lists it as an allowed parent, or when some
// type may sit anywhere. It is what decides whether a row offers Add child.
export function canHoldChildren(types: TypeShape[], name: string): boolean {
  return types.some((t) => unconstrained(t) || t.allowed_parent_types.includes(name));
}
