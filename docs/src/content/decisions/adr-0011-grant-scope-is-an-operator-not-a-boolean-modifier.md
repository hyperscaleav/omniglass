---
title: "Grant scope is an operator, not a boolean modifier"
date: 2026-07-06
status: "Accepted"
summary: "Generalize the `exclude_root` boolean into a `scope_op` operator (`subtree` / `subtree_excl_root` / `self`), a flat enum, not a predicate-expression tree"
---

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Generalize the `exclude_root` boolean ([ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind))
  into a `scope_op` operator on `principal_grant` (issue [#102](https://github.com/hyperscaleav/omniglass/issues/102)):
  `subtree` (root + descendants, the default, == old `exclude_root=false`), `subtree_excl_root` (descendants
  only for update/delete, root kept for read/create, == old `exclude_root=true`), and `self` (the root row
  only for read/update/delete, no descendants and no create-placement, a leaf-lock, net-new). The operator is a **flat enum column**, not a full predicate-expression
  tree or a per-grant tuple list. It is part of a grant's identity: the dedup index includes `scope_op`, so the
  same role at the same root with a different operator is a distinct grant.
- **Context:** Grant scope wants one composable axis, not a growing pile of booleans; the grant builder is
  already a filter-bar-style operator UI, so the operator vocabulary is the natural fit. The flat enum was
  chosen over a predicate-expression scope and a per-grant tuple list (negation, multi-root `in`): those buy
  expressiveness the boolean's two states never needed, at the cost of a much larger blast radius on the two
  authorization invariants (permission-on-every-route, scope-on-every-query). `self` is the cheap third value
  (a scalar `= any()` arm, no new recursive CTE) that turns a boolean rename into a real operator, and grant on
  exactly one node is a frequently-wanted capability the boolean could never express. The pure `scope.Resolve`
  gains a `SelfIDs` set; the three gateway walks (`inScopeTree`, `InScopeIDs`, `scopedListSQL`) gain a self arm.
  The migration also recreates the dedup index to include `scope_op`, fixing a latent collision, and threads
  `scope_op` through `RevokeGrant`'s audit SELECT (previously dropped). The operator model does **not** subsume
  the act-as scope intersection ([#101](https://github.com/hyperscaleav/omniglass/issues/101)): that blocker is
  plumbing (carry the real actor's grants and intersect two Sets per row), unchanged by how a Set is expressed.
  A future tuple model (negation, multi-root) stays a documented path if a real carve-out requirement lands.
  The console grant builder gains an operator stage (role -> kind -> entity -> operator), so [#99](https://github.com/hyperscaleav/omniglass/issues/99)
  (setting the modifier from the console) ships here too.
- **Supersedes:** [ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind) (the boolean is retired for the operator).
- **Closes the gap:** issue [#102](https://github.com/hyperscaleav/omniglass/issues/102).
