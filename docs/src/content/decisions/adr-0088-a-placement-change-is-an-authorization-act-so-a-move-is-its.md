---
title: "A placement change is an authorization act, so a move is its own verb"
date: 2026-08-08
status: "Accepted"
summary: "Placement (parent, location) leaves the component/system/location PATCH body and becomes its own `:move` custom method under its own `<resource>:move` permission, closing the gap where clearing parent_id to root via PATCH needed no scope check while creating the same root already required an all-scoped grant; MoveLocation deliberately gains no clear-to-root capability"
---

- **Date:** 2026-08-08 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [API](/architecture/api/), [identity and access](/architecture/identity-access/)
- **Decision:** `parent` and `location` leave the `PATCH` body of `component`, `system`, and `location`
  entirely (both the API input structs and the storage `Patch` structs) and become a new `POST
  /<collection>/{ref}:move` custom method, carrying `{location?, parent?}` on component and system
  (at least one required, 422 otherwise) and `{parent}` only on location. `:move` is gated by a new,
  single-word permission token, `<resource>:move`, distinct from `<resource>:update` the same way
  `:rename` is distinct from it, seeded in `internal/seed/roles.yaml` beside `rename` in the same
  slice its route lands (Operator and Deploy get `component:move`; Deploy also gets `system:move` and
  `location:move`; Administrator's `system`/`location` grants gain `move`). `:move` writes a DISTINCT
  audit verb, `move`, not the generic `update` a PATCH wrote (a loosely worded "the move is
  auditable" acceptance criterion was already satisfied by the old row, whose JSON happened to
  contain `parent_id`, without forcing anything). `:move` never calls `RecomputeHealth`: a component's
  or a system's own reparent never has, and a component's relocate never has either (a component's
  verdict is purely its own active alarms, unaffected by placement). The one exception, stated so it
  is not mistaken for an oversight: a **system's relocate** (its `location` field, not `parent`)
  keeps recomputing health at both the location it left and the one it arrived at, exactly as
  `UpdateSystem`'s combined patch already did (`TestHealthMovesOnRelocation` is the load-bearing
  proof); the health rollup runs system -> location, and a system's own location is a direct input to
  that rollup the way a component's placement no longer is post-#626, so "a placement move never
  recomputes health" holds for every reparent and every component relocate but not for this one field.
  This is a correctness statement, not a coverage claim: a **second**, known-and-tracked gap sits
  beside it. `locationVerdict` also rolls up recursively through the location tree (a system's
  location resolves upward to every ancestor, and a location's own verdict folds every system in its
  subtree downward), so a location with placed descendants that moves to a new parent leaves both the
  old and new ancestors' recorded verdicts stale, exactly the shape a system relocate closes but
  nothing closes for a location move. This is not new here: `UpdateLocation`'s old reparent branch
  never recomputed health either, so `MoveLocation` carries the gap forward rather than introducing or
  closing it, per the same "`:move` does not add new recompute calls" ruling. Tracked as
  [#642](https://github.com/hyperscaleav/omniglass/issues/642), not fixed in this task.
- **Decision (the gap this closes):** `UpdateComponent`'s and `UpdateSystem`'s old reparent branches
  guarded a rejected reparent only on the non-empty case (`if patch.ParentName != nil &&
  *patch.ParentName != ""`); an explicit empty string skipped the guard entirely and set `parent_id`
  to `NULL` with no scope check at all, while `CreateComponent`/`CreateSystem` already refused a root
  placement (`create.All` required) to a caller without an all-scoped grant. A component- or
  system-scoped (not all-scoped) principal, who could already write anything inside its own subtree,
  could clear a row's parent and walk it out of every subtree scope it had ever been placed under,
  with no check the create path itself would have refused. `MoveComponent` and `MoveSystem` now
  require `action.All` on the same branch, closing it; `TestScopedPrincipalCannotLiftToRoot` and its
  system twin are written from scratch, since every existing lift-to-root test ran with the all scope
  and none would have caught the gap or would turn red from the fix.

  The argument is written on `parent_id`, not `location_id`, because it is the true, checkable one:
  component and system scope is **own-tier only** today (a component's scope tree is its own
  ancestor chain, unrelated to a location's), so a `location_id` clear cannot lift a row out of a
  scope that never covered it in the first place, and the cross-tier cascade that would make a
  location-scope argument meaningful is a tracked later slice ([#10](https://github.com/hyperscaleav/omniglass/issues/10)).
  The durable framing that also covers that latent hole once it lands: a placement change is an
  authorization act, whatever tier it crosses, not merely a field write that happens to touch two
  more columns than a rename touches one.
- **Decision (the deliberate asymmetry):** `MoveLocation` does **not** gain a clear-to-root capability.
  `UpdateLocation`'s reparent branch never had one either (an explicit empty `ParentName` resolved
  nothing and was already a 422, `ErrParentNotFound`, before this split), so there was no gap to
  close there, and adding clear-to-root now would be a new product capability nobody asked for, not a
  security fix riding along with this task. The asymmetry with component and system, which DO gain a
  guarded clear-to-root, is intentional and stated here so a future reader does not read it as a
  missed spot.
- **Decision (the transaction split):** `MoveComponent` and `MoveSystem` are separate gateway
  functions with their own transaction and their own audit row, not a shared statement with
  `UpdateComponent`/`UpdateSystem`. `UpdateComponent`'s old UPDATE carried a three-state `CASE`
  across four columns (`display_name`, `product_id`, `location_id`, `parent_id`) in one statement;
  splitting placement out splits that statement, so an operator gesture that used to change both a
  label and a placement in one PATCH now costs two requests and writes two audit rows if the caller
  wants both. This is the same tradeoff `RenameComponent`/`RenameSystem`/`RenameLocation` already
  established for name-plus-other-field edits (ADR-0076), chosen deliberately here for the same
  reason rename earned its own act: a placement change is an authorization act, not a label edit, so
  it deserves its own grant and its own audit trail entry rather than riding along with whatever else
  a PATCH happened to touch.
- **Amended by [ADR-0092](#adr-0092-a-location-move-recomputes-both-ancestor-chains):** the
  known-and-tracked second gap this entry records (a location move leaving both ancestor chains'
  recorded verdicts stale, [#642](https://github.com/hyperscaleav/omniglass/issues/642)) is closed.
  The ruling is unchanged in principle and gains a second member of the same exception class it
  already carved out for a system's relocate: `:move` recomputes health exactly where the rollup
  genuinely depends on the placement being changed, which is a system's `location` and a location's
  `parent`, and nowhere else.
- **Context:** Task 13 of the identity-model epic ([#627](https://github.com/hyperscaleav/omniglass/issues/627)).
  The two storage placement test files (`components_placement_test.go`, `systems_placement_test.go`)
  and the placement end-to-end tests moved to the new verb rather than being deleted; the compiler
  found every storage caller once the `Patch` struct fields were removed, the HTTP end-to-end callers
  (`map[string]any` bodies, resolved only at request time) did not, and were found by re-reading every
  `PATCH .../parent` and `PATCH .../location` call site by hand. `location:checkName` is unaffected:
  it is the advisory placement-availability precheck, unrelated to `:move`, and was not touched.
- **Tracked under** epic [#627](https://github.com/hyperscaleav/omniglass/issues/627).
