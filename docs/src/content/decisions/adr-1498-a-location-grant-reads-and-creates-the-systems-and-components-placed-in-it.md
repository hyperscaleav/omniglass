---
title: "A location grant reads and creates the systems and components placed in it"
date: 2026-11-15
status: Accepted
summary: "A subtree-op location grant reaches the system and component resources for read and create as a cross-tier root (scope.Set.LocationIDs), matched by a row's placement, never by its own ancestry; a root create places inside it, a root with no location still needs an all-scoped grant; the modify actions, the group kind and every other resource stay on their own tier (#10)"
refs: [1498, 10, 714, 700, 1981]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** (#1498, the create and read half of #10.)
  1. **A cross-tier root, not a same-tier id.** `scope.Resolve` puts a location grant's root in a new `Set.LocationIDs` when the resource is `system` or `component`, the action is `read` or `create`, and the grant's op is `subtree` or `subtree_excl_root` (both keep the root for read and create, as they already do on their own tier). `applicableKinds` and `Covers` are unchanged, so the location id is never compared against a system or component's own ancestor chain, where it could never match.
  2. **Matched by placement.** The Storage Gateway expands the root to its location subtree and admits a system placed in it plus that system's subsystems, and a component whose own location is in it, or with none of its own whose primary system is, plus its subcomponents (the effective place of ADR-0140). The match lives in the scope-tree primitives (`inScopeTree`, `inScopeIDs`, `scopedListSQL`, and the read filter every derived component read uses), so a list, a get, a parent bind and the alarms, notifications, dependencies and fleet dots of a component all answer alike.
  3. **Create places inside the root.** A root system or component is admitted with an all-scoped create grant, as before, or with a location inside a cross-tier root of the create scope. A root with no location still needs the all-scoped grant (403). The location is still bound within the caller's location read scope first (#700's non-disclosing 422 is unchanged), so a location outside the grant answers as it did.
  4. **What does not move.** update, rename, move and delete on a system or component; every resource that hangs off a component (endpoint, task, alarm, command, notification, telemetry); a `self` location grant; a `system` or `component` grant's reach into locations; and the `group` kind. The queue-time notification recipient filter (`Set.AdmitsChain`) ignores `LocationIDs` and fails closed (#1981).
- **Context:** The dev seed's `tech-east` (deploy at `east`, `subtree_excl_root`) could not create a system or component in the rooms it builds out, or read one, so a field tech needed a location, a system and a component grant together (#714 documented the gap). Read and create are the half a field tech needs to build a room out; the modify half needs a placement-aware rule for moving a row out of the grant, which is #10.
- **Not chosen:**
  - **Adding `location` to `applicableKinds("system")` and `("component")`.** The ancestry walk compares a grant's id against the row's own tier chain, which holds no location id, so it would match nothing, and it would grant every action, the modify ones included.
  - **Resolving the location to system and component ids at grant time.** The set would go stale as rows are created and moved, and it grows with the fleet rather than with the grants.
