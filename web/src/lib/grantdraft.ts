// grantdraft is the pure staging core behind the grant builder. The admin builds a
// desired grant set (the draft) on top of the principal's current server grants;
// nothing is sent until save. These framework-agnostic functions compute the
// draft, its per-chip visual state, the add/remove diff a save applies, and the
// stage-time validation, so the builder component is a thin shell over tested
// logic (the same split as predicate.ts behind FilterBar). The invariant: a grant
// is identified by role + scope, not its server id, so a draft can be diffed
// against the current set to know exactly what to POST and what to DELETE.

import type { ScopeKind } from "./principals";

// A grant identity: role at a scope. scope_id is absent for the all scope.
export type GrantRef = { role: string; scope_kind: ScopeKind; scope_id?: string };
// A current (server) grant additionally carries its id, needed to DELETE it.
export type ExistingGrant = GrantRef & { id: string };

// grantKey identifies a grant by role and scope, ignoring the server id and the
// phantom scope_id the all scope never uses, so it is the basis for dedup and diff.
export function grantKey(g: GrantRef): string {
  return g.scope_kind === "all" ? `${g.role}@all` : `${g.role}@${g.scope_kind}:${g.scope_id ?? ""}`;
}

// draftFromGrants seeds a draft as a copy of the current grants (nothing changed
// yet): each existing grant is present and kept.
export function draftFromGrants(current: ExistingGrant[]): GrantRef[] {
  return current.map((g) => ({ role: g.role, scope_kind: g.scope_kind, scope_id: g.scope_id }));
}

// stageGrant appends a grant to the draft, deduped by key (staging an already
// present role@scope is a no-op).
export function stageGrant(draft: GrantRef[], add: GrantRef): GrantRef[] {
  const key = grantKey(add);
  if (draft.some((g) => grantKey(g) === key)) return draft;
  return [...draft, add];
}

// toggleGrant flips a grant's membership in the draft by key: a present grant
// (kept existing or pending add) is dropped; an absent one is restored from the
// current set (undo a removal). Toggling an absent key with no current match is a
// no-op.
export function toggleGrant(current: ExistingGrant[], draft: GrantRef[], key: string): GrantRef[] {
  if (draft.some((g) => grantKey(g) === key)) return draft.filter((g) => grantKey(g) !== key);
  const restore = current.find((g) => grantKey(g) === key);
  return restore ? [...draft, { role: restore.role, scope_kind: restore.scope_kind, scope_id: restore.scope_id }] : draft;
}

// A chip's state relative to the current server set: unchanged (kept), removed
// (existing but marked for removal), or added (staged, not yet saved).
export type ChipState =
  | { kind: "unchanged"; grant: ExistingGrant }
  | { kind: "removed"; grant: ExistingGrant }
  | { kind: "added"; grant: GrantRef };

// chipStates renders the draft as one ordered list for the chip row: every current
// grant first (unchanged if still in the draft, removed if not), then the pending
// adds in the order they were staged.
export function chipStates(current: ExistingGrant[], draft: GrantRef[]): ChipState[] {
  const draftKeys = new Set(draft.map(grantKey));
  const currentKeys = new Set(current.map(grantKey));
  const out: ChipState[] = current.map((g) =>
    draftKeys.has(grantKey(g)) ? { kind: "unchanged", grant: g } : { kind: "removed", grant: g },
  );
  for (const g of draft) {
    if (!currentKeys.has(grantKey(g))) out.push({ kind: "added", grant: g });
  }
  return out;
}

// pendingDiff is what a save applies: adds are draft grants absent from the current
// set; removes are current grants absent from the draft (carrying their ids).
export function pendingDiff(current: ExistingGrant[], draft: GrantRef[]): { adds: GrantRef[]; removes: ExistingGrant[] } {
  const draftKeys = new Set(draft.map(grantKey));
  const currentKeys = new Set(current.map(grantKey));
  return {
    adds: draft.filter((g) => !currentKeys.has(grantKey(g))),
    removes: current.filter((g) => !draftKeys.has(grantKey(g))),
  };
}

// isDirty reports whether the draft differs from the current set (any add or
// remove), i.e. whether save has anything to do.
export function isDirty(current: ExistingGrant[], draft: GrantRef[]): boolean {
  const { adds, removes } = pendingDiff(current, draft);
  return adds.length > 0 || removes.length > 0;
}

// A stage-time rejection reason, surfaced before a grant is added to the draft.
export type StageError = "role-required" | "entity-required" | "duplicate";

// validateStage checks a would-be grant against the draft: a role is required, a
// scoped kind needs an entity, and the role@scope must not already be staged.
export function validateStage(draft: GrantRef[], candidate: Partial<GrantRef>): StageError | null {
  if (!candidate.role) return "role-required";
  const kind = candidate.scope_kind ?? "all";
  if (kind !== "all" && !candidate.scope_id) return "entity-required";
  const key = grantKey({ role: candidate.role, scope_kind: kind, scope_id: candidate.scope_id });
  if (draft.some((g) => grantKey(g) === key)) return "duplicate";
  return null;
}
