import type { EntityKind, Tag } from "./tags";

// tagdraft: the pure core of the TagAdder. It decides, with no I/O, which
// registry keys the key-stage should suggest for an entity, whether a typed key
// already exists (so the value stage can proceed) or would be a new key (the
// coin-new-key path), and whether a value is bindable. The component is a thin
// shell over these functions, so the picking and validation logic stays
// unit-testable in isolation.

// keyApplies reports whether a key may be bound onto an entity of `kind`: an
// empty applies_to is universal, otherwise the kind must be listed. Mirrors the
// server's applies_to gate, so the picker never offers a key the write rejects.
export function keyApplies(tag: Tag, kind: EntityKind): boolean {
  return tag.applies_to.length === 0 || tag.applies_to.includes(kind);
}

// keySuggestions returns the registry keys to offer in the key stage: those that
// apply to the entity kind, are not already bound on it, and match the query as a
// case-insensitive substring, ordered by name. An empty query lists them all.
export function keySuggestions(all: Tag[], kind: EntityKind, bound: string[], query: string): Tag[] {
  const q = query.trim().toLowerCase();
  const boundSet = new Set(bound);
  return all
    .filter((t) => keyApplies(t, kind))
    .filter((t) => !boundSet.has(t.name))
    .filter((t) => q === "" || t.name.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name));
}

// exactKey returns the registry key whose name equals the query exactly (case
// sensitive, since keys are normalized lowercase), or undefined. A match means
// the key exists and the value stage can proceed; no match with a non-empty
// query is the coin-new-key candidate.
export function exactKey(all: Tag[], query: string): Tag | undefined {
  const q = query.trim();
  return all.find((t) => t.name === q);
}

// canCoin reports whether the typed query is a candidate to mint as a new key:
// non-empty, not already an existing key, and the caller may create keys. The
// name's own validity is enforced by the create form and the server.
export function canCoin(all: Tag[], query: string, mayCreate: boolean): boolean {
  const q = query.trim();
  return mayCreate && q !== "" && !exactKey(all, q);
}

// valueValid reports whether a value is bindable: non-empty after trimming. The
// length ceiling and any normalization are the server's to enforce.
export function valueValid(value: string): boolean {
  return value.trim() !== "";
}
