---
title: "Changing a system's standard carries its staffing onto the roles that still exist"
date: 2026-10-26
status: Accepted
summary: "When a system's standard changes, each assignment to a role of the outgoing standard moves onto the role of the same name the new standard declares, keeping its position where it is free, when the component is still a typed match for that role and the role has room under its capacity; every other one is released. A released component stays a member (giving up a role never ends membership), so it is never wedged: it can be unbound, restaffed and deleted. Clearing the standard releases every inherited assignment; the system's own roles keep theirs. Each move and release is audited in the change's transaction and the system's health recomputes after it. Staffing writes take a per-system advisory lock shared and a standard change takes it exclusive, so they serialize, and a one-time backfill releases the assignments a change already stranded"
refs: [506, 504, 503]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [api](/architecture/api/)
- **Decision:** (#506, the architect's ruling in session for the 0.1.0 release, #825: migrate the
  roles that still exist, release the ones that do not, and never wedge the component.)
  1. **A role that still exists keeps its staffing.** On a standard change, an assignment to a role
     the outgoing standard declares moves onto the role of the same name the new standard declares,
     in the same transaction as the repoint. The row is moved, not replaced, so its id and its audit
     history hold. It keeps its position when that position is free in the new role and within its
     capacity, and takes the lowest free one when it is not.
  2. **Only a component that still fits moves.** The typed-slot guard runs again against the new
     role (its accepted types and pinned products), and the new role's capacity is respected,
     lowest positions first. A role of the same name that wants something else is a different
     requirement, so a component that no longer satisfies it is released, never staffed into a
     mismatch.
  3. **Everything else is released, and released means a member with no role.** A role the new
     standard does not declare, a component that no longer fits, and an occupant past the new
     capacity all lose the assignment and keep the membership, so the component can be unbound,
     restaffed or deleted at once. Clearing the standard (a one-off system) releases every
     inherited assignment; roles declared on the system itself are not touched by any of this.
  4. **Audited and recomputed.** Each move is an `update` and each release a `delete` of the
     assignment in the audit log (keyed on the assignment's id, its images naming the system, both
     standards, the role, the component and the position), written in the change's transaction, and
     the system's health recomputes after the staffing has settled, so the recorded verdict already
     counts the carried occupants.
  5. **Serialized against staffing.** Every staffing write (assign, unassign, swap) takes a
     per-system staffing lock (an advisory lock keyed on the system's id) shared before it resolves
     the role, and a standard change takes it exclusive, after the role-name lock and before it
     touches the system's row. A staffing write either finishes before the change reads the
     assignments or starts after the change commits and resolves against the new standard; neither
     can leave an assignment on the outgoing standard behind. It is an advisory lock and not the
     system's row `FOR SHARE`: every label cascade takes the label lock and then system rows for
     update, and a staffing write takes the label lock after this one, so a row lock closed a cycle
     with every cascade (the adversarial review measured it deadlocking two rounds in three). A role
     withdrawal and a standard's delete take the role-name lock a standard change takes
     ([ADR-0503](#adr-0503-a-role-name-addresses-one-role-per-system)), so neither interleaves with
     a carry, and the delete also holds the standard's own row across its in-use count, which a
     system created onto the standard waits on through its foreign key. A withdrawal waits for the
     carry; a delete either sees the system that moved or was created onto the standard and
     refuses, or runs first and the write onto it fails as an unknown standard. The foreign key
     would otherwise null that system's standard, a standard change that skipped the carry. Every
     step of a standard change keys on the change's own locked decision, never on comparing an
     unlocked read with the row it wrote, so a save that resent the standard it read while a change
     committed carries and recomputes nothing of its own.
  6. **The strands already made are released once.** A data backfill deletes every assignment whose
     role is neither the system's own nor its current standard's. Those rows have been invisible to
     the roles read and to health since their standard moved, so releasing them changes no verdict
     and no visible staffing; it only frees the components they wedged.
- **Context:** `UpdateSystem` repointed `standard_id` and left every assignment on the old
  standard's roles. Reproduced live: the assignment vanished from the roles read, unassigning it was
  a 404 (the resolver joins only the current standard), unbinding the member a 409 (it still counted
  as staffing), and deleting the component was refused by the assignment's restricting foreign key,
  while moving back silently reactivated the staffing without re-running the guard. The issue
  argued against matching by name because it restaffs on a collision
  ([#503](https://github.com/hyperscaleav/omniglass/issues/503)); that objection is closed by
  [ADR-0503](#adr-0503-a-role-name-addresses-one-role-per-system), under which a name means one role
  per system, and by running the guard again on every move.
