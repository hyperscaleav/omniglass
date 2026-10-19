// The entity name rule, mirrored from storage (#586: one rule for every
// name-bearing row): lowercase letters, digits and hyphens, starting with a
// letter or digit, at most 100 characters, never a dot, and never shaped like
// a uuid (an id would be read as a name, or a name as an id, ADR-0062). The
// server judges every write; forms judge with this first so they can say why
// before sending.
export const ENTITY_NAME_RE = /^[a-z0-9][a-z0-9-]*$/;
export const ENTITY_NAME_MAX = 100;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

// entityNameError says why a name breaks the rule, or null; empty is the
// caller's concern (a required field says so its own way).
export function entityNameError(name: string): string | null {
  if (!name) return null;
  if (name.length > ENTITY_NAME_MAX) return `At most ${ENTITY_NAME_MAX} characters.`;
  if (!ENTITY_NAME_RE.test(name)) return "Lowercase letters, digits and hyphens, starting with a letter or digit.";
  if (UUID_RE.test(name)) return "A name cannot be shaped like a uuid; ids and names are told apart by that shape.";
  return null;
}
