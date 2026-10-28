---
title: "The boot seed installs a shipped role whole, and repairs a set an unrevised role is missing"
date: 2026-10-26
status: Accepted
summary: "SeedSystemRole writes a shipped role and its typed-slot sets (accepted types, pinned products) in one transaction, so a role never lands without them. On every later boot it repairs: a shipped role the seed itself wrote (no audit row names it, since every operator write of a role is audited and the seed never is) and nobody has revised (its updated_at still equals its created_at) whose accepted-types or pinned-products set is entirely empty while the shipped declaration names some gets the shipped set, which is what a complete install would carry. A set with any rows, any role an operator declared, and any role an operator has revised are left exactly as they stand, so an edit still survives every restart"
refs: [508, 504]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/)
- **Decision:** (#508, the architect's ruling in session for the 0.1.0 release, #825: make the seed
  transactional and self-repairing.)
  1. **Whole or not at all.** The role row and its typed-slot sets are written in one transaction,
     the way `SetSystemRole` already wrote them, so a fault between the statements (a killed first
     boot, a reset connection, a shipped declaration naming a type the registry lacks) leaves
     nothing behind rather than a role that accepts anything.
  2. **Repair what only a partial install leaves.** On a boot that finds the role already present,
     the seed reads it instead of returning unread. A role the seed wrote and nobody has revised
     that holds **no** accepted type while the shipped declaration names some receives the shipped
     accepted types; the same for pinned products. That is exactly the state the old two-statement
     seed could leave, and it heals on the next boot. "Nobody has revised" is `updated_at` equal to
     `created_at` (`SetSystemRole`'s update bumps it); "the seed wrote it" is that no audit row
     names the role, because every operator write of a role is audited in its own transaction and
     the seed never is. The timestamps alone cannot tell: a role an operator declared through
     `SetSystemRole`'s insert and never revised carries equal timestamps too, and an empty set when
     it accepts any type, so a release shipping a role of the same name would otherwise fill it
     (the adversarial review reproduced exactly that).
  3. **An edit still wins.** A set holding any rows is never touched, and a role an operator
     declared or revised is never touched at all, so retuning a shipped role survives every restart as before
     ([the seed model](/architecture/core-entities/#the-seed-model-templates-versus-canonical-catalogs)).
     The accepted edge: a later release that adds a requirement to a shipped role that shipped
     with none fills it in on an install nobody has edited, which is what a fresh install of that
     release carries anyway.
- **Context:** `SeedSystemRole` ran the role insert and its typed-slot inserts as separate pool
  calls, and on a later boot `ON CONFLICT DO NOTHING` returned without reading the role. Reproduced
  live: a role whose capability insert failed was committed with no requirement, and seeding it
  again after the registry was fixed returned nil and left it empty for good, so the assignment
  guard accepted any component into the slot while health counted it against quorum. The typed-slot
  model (#626) replaced capabilities since the issue was filed; the window and the missing repair
  were the same.
