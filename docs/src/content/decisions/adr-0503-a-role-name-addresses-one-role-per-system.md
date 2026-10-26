---
title: "A role name addresses one role per system"
date: 2026-10-26
status: Accepted
summary: "A system's roles are its standard's plus its own, and every staffing route addresses a role by name, so a name must mean one role in a system. A write that would give a system two roles of one name is refused with a 409 naming the role, the standard and the system: a system declaring a name its standard declares, a standard declaring a name a conforming system declares itself, and a system moving onto a standard that declares a name it declares itself. Shadowing is not an override, after #443's ruling for the registries: a name in both is a collision, and picking a winner is the bug. The boot seed defers to the operator (a shipped role a conforming system already declares itself is not installed), and a staffing write that meets a collision written before this rule is refused naming both roles rather than binding one arbitrarily. Every write that can create a collision serializes on one advisory lock"
refs: [503, 504, 443]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [api](/architecture/api/)
- **Decision:** (#503, the architect's ruling in session for the 0.1.0 release, #825.)
  1. **A name means one role per system.** A system's effective roles are the ones its standard
     declares plus the ones declared on it, and the staffing routes, the console and health all
     address a role by its name within the system. A write that would leave a system with two roles
     of one name is refused with a 409 naming the role, the standard and the system, on every path
     that can create one: declaring on a system a name its standard already declares, declaring on a
     standard a name a conforming system already declares itself, and changing a system's standard
     to one that declares a name the system declares itself.
  2. **Shadowing is refused, not supported as an override.** An override would need the system's
     role to replace the inherited one in the health rollup and in staffing, and the resolver to
     prefer one arc deterministically: a second semantics nobody asked for, and the one the
     reproduction showed is easy to get half right. The registries settled the same question in
     #443: a name present in both is a collision, and a collision resolves to nothing. An operator
     who needs a second slot names it.
  3. **The boot seed defers.** Shipped roles are seed-if-absent example content the operator owns
     ([the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)),
     so a shipped role that a conforming system already declares itself is not installed, rather
     than failing the boot or shadowing the operator's role.
  4. **A collision written before this rule is refused, never resolved silently.** The resolver
     every staffing write takes reads every role of the name and refuses a second match naming both,
     instead of binding whichever row the database returned first; withdrawing the system's role
     (which is arc-scoped and always worked) resolves it.
  5. **Serialized.** The check reads one table and writes another, so every write that changes which
     roles a system has (both arcs' declarations and withdrawals, a standard change, the role seed,
     a standard's delete) takes one advisory lock first, before any other lock its transaction
     takes. An edit that resends a system's current standard, as the console does on every save, is
     not a standard change and takes no part in it, so the lock serializes only the rare acts it is
     for.
- **Context:** `system_role_name_key` is unique per arc, so `('standard', std, NULL, 'table-mic')`
  and `('system', NULL, sys, 'table-mic')` were distinct rows, and `resolveRole` matched both arcs on
  name with no ordering and read one row. Reproduced live: a system role declared over an inherited
  one was accepted; staffing it landed on the standard's role; a component that exactly matched the
  ad-hoc role was refused naming requirements it never had; and the shadowed slot, unstaffable,
  held the system's health at its worst impact for good. The rule also makes
  [ADR-0506](#adr-0506-changing-a-systems-standard-carries-its-staffing-onto-the-roles-that-still-exist)'s
  match by name sound: a name that means one role on each side of a standard change cannot restaff
  onto the wrong one.
