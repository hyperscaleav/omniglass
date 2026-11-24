// Plain words for what the profile's Access card shows (#2447): a grant's scope
// as a sentence fragment instead of "owner @ all", and the one permission that is
// not a permission but a wildcard (">", every permission) as what it means.

export function scopeWords(g: { role: string; scope_kind: string; scope_id?: string }): string {
  if (g.scope_kind === "all") return "Everywhere";
  if (g.scope_kind === "location" || g.scope_kind === "system" || g.scope_kind === "component") return `One ${g.scope_kind}`;
  return g.scope_kind;
}

export function permissionLabel(p: string): string {
  return p === ">" ? "Every permission" : p;
}
