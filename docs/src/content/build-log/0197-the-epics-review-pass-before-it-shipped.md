---
title: "The epic's review pass, before it shipped"
date: 2026-08-11
refs: [657]
form: bullet
---

- **The epic's review pass, before it shipped** ([#657](https://github.com/hyperscaleav/omniglass/issues/657)).
  Four defects found reading the eight slices as one diff, each fixed with the test that failed
  first, and each of a kind the slice that introduced it could not have seen from inside itself.

  **A deleted system left every member component's label stale.** `system_member_system_id_fkey` is
  `ON DELETE CASCADE`, so deleting a system had always taken its memberships with it, inside the
  parent row's own `DELETE` where the gateway can hook nothing. Slice 5 derived the label write paths
  from the data map and caught every EXPLICIT mover of a primary membership; this is the one the
  database performs. A component in a boardroom under `[{{.SystemTypeLabel}}]` went on reading
  `[Boardroom]` after the boardroom was gone, and the epic's own invariant said so, which is the
  property it exists for. The memberships are now released explicitly one step ahead of the row's
  delete, so a delete is the same act `RemoveMember` performs and carries the same two consequences:
  the sole surviving membership is promoted to default (a divergence between the two doors into
  `system_member` that predated the epic, fixed here because the label the delete leaves behind
  depends on which membership answers afterwards), and the released components restamp in one
  recompute. The generic scoped delete grew a `beforeDelete` hook to hold it. The invariant fixture
  now drives a delete too: its claim that a write path nobody thought of fails it only ever reached
  the acts the fixture performs, and all fifteen of them were writes.

  **A `:move` that changed no bucket renamed a platform-named system.** The re-mint was gated on the
  row being platform-named alone, so a move that re-stated the location a system already sat at, or
  supplied neither field, re-entered the generator; with a lower ordinal freed by an earlier
  `:rename` that does not recompute to the same answer, it MOVES the name, under `system:move` with
  no rename asked for. It is the reclassify defect ADR-0101 already refused, wearing the other verb's
  clothes. The location arm shipped narrow one slice later and recorded the system arm as
  deliberately wide "because narrowing it would change an existing expected value"; the premise was
  false, since systems had only had generated names since this same unmerged branch. Narrowed, on the
  `nameScope` bucket rather than the two pointers, because a parent wins over a location. No test
  expectation moved. `MoveComponent` has the identical shape, is reachable the same way, and is
  genuinely pre-existing, so it is left for its own issue.

  **A platform-named location could not be reclassified, and neither the message nor the guide said
  so.** The refusal is correct and stays: re-minting is what a reclassify does to a name the platform
  owns, and a type with no rule leaves nothing to mint from. But `floor` is the only shipped type
  carrying a rule, so reclassifying a generated floor as a room, a building or a campus is the
  routine misclassification fix, and it is refused. The message said "name it yourself", advice with
  nowhere to take it, since the patch that reclassifies carries no name field; it now names the
  escape the way the system tier's twin does, and the operator guide documents the two-step fix where
  an operator meets it.

  **A label preview listed rows the apply would then refuse to touch.** The verb's two halves were
  wired to different narrowings: the apply selects on the caller's read scope AND their update scope,
  both injected into the one query, while the preview passed an all-scope action set that made the
  second predicate a constant. So an operator with estate-wide read and a narrow grant to update was
  shown a blast radius the apply then declined most of, with nothing on the wire explaining the gap.
  Both ADR-0100 and the recompute file's own argument for why a preview is an apply that rolls back
  already said a preview must list EXACTLY the rows the apply changes, so the scoping was the one
  place the code disagreed with the reason it exists. The preview now takes the same two scopes the
  apply does, and the e2e case that pinned the old answer (a preview of two rows against an apply of
  one) pins the new invariant instead: the two agree, row for row.
