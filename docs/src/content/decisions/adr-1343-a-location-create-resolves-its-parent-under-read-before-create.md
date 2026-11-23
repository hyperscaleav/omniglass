---
title: "A location create resolves its parent under read before create"
date: 2026-11-23
status: "Accepted"
summary: "POST /locations (and its batch create and the template plan) resolves the parent under location:create first and, when that refuses, under location:read to pick the refusal: a parent the caller cannot read and a parent that does not exist are one 404 `parent location not found`, byte for byte; a readable parent outside the create scope stays 403. Revises ruling 2 of #627 for the location create path; the parent-not-found sentinel maps to 404, not 422."
refs: [1343, 627, 1328, 1964]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#the-three-way-status-split)
- **Decision:** (#1343)
  1. **The parent's refusal follows the three-way split.** A location create resolves its parent
     within the caller's `location:create` scope, with ambiguity judged inside that scope, as
     before. When that resolve refuses, the gateway asks the read question with the
     non-disclosing resolve: a parent inside `visible_set(P, read)` is the truthful **403**
     (`ErrLocationForbidden`), and a parent outside it, or nowhere at all, is
     `ErrParentNotFound`. Root creation (no parent) is unchanged: it needs an all-scoped create
     grant, else 403.
  2. **`ErrParentNotFound` is a 404.** `mapLocationErr` answers `404 parent location not found`
     where it answered 422. One status and one body for both cases is what makes them
     indistinguishable; 404 was chosen over a shared 422 to match `POST /locations:instantiate`,
     whose target location already resolves under `location:read` and answers 404 for both.
  3. **The read set grants nothing.** It only decides which refusal is owed, so a wide read cannot
     widen a narrow create, and an `*ErrAmbiguousName`'s candidates still never name a location
     outside the create scope.
  4. **One resolver for every location create.** `resolveCreateParent` in
     `internal/storage/locations.go` serves `CreateLocation`, `BatchCreateLocations` and a plan's
     locations (`createLocationTx`), each now carrying the caller's `location:read` scope.
- **Context:** Ruling 2 of [#627](https://github.com/hyperscaleav/omniglass/issues/627) ("scope
  decides before ambiguity does") resolved the parent under the create scope alone and kept the
  not-found versus forbidden split of `resolveScopedRef`, on the argument that a reference the
  caller supplied in the same request is not a new disclosure. That holds for a row the caller can
  read and fails for one it cannot: any `location:create` holder could ask for a parent by name and
  learn from 403 versus 422 whether a location outside its read exists. The security review of the
  instantiate routes ([#1328](https://github.com/hyperscaleav/omniglass/issues/1328)) found it.
- **Consequences:** The status change reaches every route that maps `ErrParentNotFound` through
  `mapLocationErr`: a `:move` to a parent that does not exist, and a `:renderLabel` draft naming a
  missing parent, now answer 404 where they answered 422. Their scope resolve is unchanged here, as
  is component and system create: each still answers an existing parent outside its own scope with
  a 403, and bringing them under the same split is
  [#1964](https://github.com/hyperscaleav/omniglass/issues/1964).
