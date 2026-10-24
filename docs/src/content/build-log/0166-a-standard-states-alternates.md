---
title: "A standard states alternates"
date: 2026-08-08
refs: [626]
form: bullet
---

- **A standard states alternates** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `1f1de87`, `9817058`, `29810a5`). A role can join an exclusive-or group instead of contributing
  to its system unconditionally: `role_choice` is the group, `choice_alternate` one way to
  satisfy it, `system_role.alternate_id` how a role joins one. The pure fold
  (`internal/health.Alternate` / `Choice` / `SystemVerdictWith`) picks each choice's
  best-satisfied alternate by declaration position and folds only its roles, so an all-in-one
  room's satisfied video bar no longer takes the system down over its component-built
  alternate's five unbuilt roles. `alternate_id` is `ON DELETE RESTRICT`, never `SET NULL`:
  deleting an alternate would otherwise silently promote every attached role from conditional to
  mandatory, the same trade already made when a plain `ON DELETE SET NULL` was rejected at schema
  level; `DeleteChoice`/`DeleteAlternate` refuse instead, naming the roles still attached. A
  composite ownership FK closes the cross-arc hole where a role could join a foreign owner's
  alternate, and a same-slice repair closes `system_role`'s own missing owner-arc CHECK (an
  unknown standard or system name used to resolve to NULL and succeed silently, creating an
  ownerless role a second typo would then update; `role_choice` would have inherited the same
  hole). Two review rounds followed. `9817058` found `SetSystemRole` wholesale-replacing
  `alternate_id` on every write with no route ever populating it, so any PUT through the existing
  role routes, including the console's own save, silently detached a role from its alternate; the
  health report also carried the choice-aware verdict beside an ungrouped role list, so a
  satisfied choice's unbuilt alternate read as ordinary impaired roles with nothing marking them
  as not why, closed by `choice`, `alternate` and `active` on `HealthRole`. `29810a5` found the
  boot-seed reconciliation incomplete: it converged declared alternates onto their positions but
  never touched a stored row whose name had dropped out of the declared set, so a rename or a
  drop against an already-seeded database still collided on `choice_alternate_position_key` and
  aborted server boot. The seed now reconciles the stored alternate set to the declared one every
  boot, existing rows included, **deleting** any alternate no longer declared unless a role still
  points at it, in which case the whole call refuses with `ChoiceInUseShortfall` rather than let
  the FK's RESTRICT surface as a bare constraint violation or silently detach a role: refusing to
  boot beats silently detaching a role, the same trade the `ON DELETE RESTRICT` call above already
  made. This is a narrow carve-out from the boot-seed doctrine's usual insert-if-absent rule,
  safe here only because `choice_alternate` has no operator write path and its positions are a
  packed sequence where an orphan actively collides; recorded in
  [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability).
