---
title: "Role is IAM's; what a system needs filled is a `system slot`"
date: 2026-11-16
status: "Accepted"
summary: "Role keeps its IAM sense alone. The place in a system a component fills is a system slot on every surface; system role retires and the role disambiguation row goes with it"
refs: [2032, 2028, 2061]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [core entities](/architecture/core-entities/), [health](/architecture/health/),
  [identity and access](/architecture/identity-access/)
- **Decision:** (#2032.) **Role** means one thing: the IAM permission bundle granted to a
  principal (`/roles`, the `role` table, `role:read:admin`, the Admin Roles page), unchanged. The
  place in a system that a component fills is a **system slot**, on every surface: the schema
  (`system_slot`, `system_slot_assignment`, `system_slot_type`, `system_slot_product`,
  `slot_choice`), the API (`/standards/{id}/slots`, `/systems/{name}/slots`, the assignment and
  `:swapPositions` routes under them), the CLI subgroups, the expression accessor (`$slot`), the
  Go names and the console and docs. The name is owner-qualified, as `system_role` was: a slot of
  a system, which a component fills. Prose says **slot** where the system is already in view
  (fill the slot, an empty slot, the slot's quorum and impact) and **system slot** on first use.
  **System role** retires and joins the docs denylist; the glossary's role disambiguation row is
  removed rather than kept. Quorum, impact, impaired, position and choice keep their meanings. The
  renames are #2061.
- **Context:** the glossary carried a disambiguation row because role named two unrelated things,
  a permission bundle and a staffing need, and the second sense owned most of the uses: 59 docs
  hits for "system role" against 6 for "IAM role", five tables, six API paths, two CLI subgroups
  and roughly 500 Go identifiers (`AssignRole`, `SetSystemRole`, `SystemRoleSpec`). IAM keeps the
  word because RBAC, OIDC role claims and every peer product use it; moving it would teach an
  operator a word nobody else uses. The docs already defined a system role as "a slot a system
  needs filled" and its guard as the typed-slot guard, so slot is the word the model was explaining
  itself with. It is also the only candidate that names the vacancy rather than the occupant, which
  is what a commissioning gap is: an empty slot. Considered: **position**, already the ordering of
  assignments within a slot (`:swapPositions`); **seat**, a conference delegate unit in the fleet
  and a licensing term; **duty**, **function** (the expression engine's functions) and **part**
  (part numbers beside products); **post** (HTTP POST, and an echo of outpost, ADR-2031);
  **member**, which names the occupant; and **component slot**, which head-noun-last reads as a
  slot on a component, the chassis card-slot sense. That sense does not exist in the model today;
  if frame composition arrives it takes its own owner-qualified noun (a chassis bay) and never the
  bare word.
