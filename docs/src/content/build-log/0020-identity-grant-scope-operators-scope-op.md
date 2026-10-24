---
title: "Identity: grant scope operators (`scope_op`)"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: grant scope operators (`scope_op`).** The `exclude_root` boolean generalizes into a `scope_op`
  operator on `principal_grant` (ADR-0011): **`subtree`** (root + descendants, the default, == old
  `exclude_root=false`), **`subtree_excl_root`** (descendants only for update/delete, root kept for
  read/create, == old `exclude_root=true`), and **`self`** (exactly the root row for read/update/delete, no
  descendants and no create-placement, a leaf-lock) which is **net-new**, a grant on exactly one node. The pure `scope.Resolve` gains a `SelfIDs`
  set (matched by id equality, never subtree-expanded; a `self` grant re-adds a root a `subtree_excl_root` grant
  stripped), and the three gateway walks (`inScopeTree`, `InScopeIDs`, `scopedListSQL`) gain a self arm. The
  migration recreates the dedup index to include `scope_op` (fixing a latent collision where two grants
  differing only by operator clashed) and threads `scope_op` through `RevokeGrant`'s audit SELECT (previously
  dropped). The **grant builder** gains an operator stage (role -> kind -> entity -> operator, subtree
  pre-selected), and each scoped chip shows its operator glyph (= / ≥ / >), so #99 (setting the modifier from
  the console) ships here too. Proven by scope unit tests (self classification, self re-adds an excluded root,
  self-under-subtree is redundant), a storage round-trip (bad operator refused, a self grant persists alongside
  a subtree grant on the same root), a real-binary API test (a `self` grant reads and updates its node,
  descendants 404, list returns only the node), and web unit + component tests (operator in the grant identity,
  an operator change diffs as revoke + grant, staging a self grant sends `scope_op: self`). The act-as scope
  intersection (#101) is **not** subsumed: it is plumbing, orthogonal to how a scope is expressed.
