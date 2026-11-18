---
title: "A component with endpoints is occupied: its delete is refused, not cascaded"
date: 2026-11-11
status: "Accepted"
summary: "A component delete is refused (409) while the component owns endpoints, as it already is while it has child components. Cascading the endpoints and their tasks away with the component is the entity-lifecycle story (#528), not taken here"
refs: [528, 138, 114]
---

- **Date:** 2026-11-11 | **Status:** Accepted | **Pages:** [data collection](/architecture/collection/#endpoints-the-devices-apis-declared-once), [core entities](/architecture/core-entities/)
- **Decision:** (#528, raised in #138.) The component's delete counts the endpoints that name it (`endpoint.component`) in the same transaction and refuses with a 409 when there are any, naming the count and the next step (delete or reassign them first). It is a distinct error, `ErrComponentHasEndpoints`, so the message is true: `ErrComponentOccupied` says "has child components".
- **Consequences:** No endpoint, and no task derived from it, is orphaned into the all-scope-only bucket by a component delete. An operator deletes or reassigns a component's endpoints before the component. The foreign key stays `ON DELETE SET NULL`: with the refusal in front, it is unreachable from the component delete, and changing it is a schema change nothing needs. The one in-tree caller that deleted a component with endpoints (the rollback of a failed simulated-component add) now deletes its endpoints first. Occupancy rules for systems and locations are unchanged.
- **Context:** Came up in #138, found in the authz review of the reachability slice (#114). The delete narrowed access rather than widening it, so it was not an escalation, but it was silent data orphaning: the old check counted only `parent_id` children.
- **Not chosen:**
  - **Cascade-delete the endpoints and tasks with the component.** It destroys collection configuration and history as a side effect of a delete that names none of it. That is the delete-cascade design tracked by #528, and it should be decided once for every entity rather than per table here.
  - **Widen `ErrComponentOccupied` with a reason.** One sentinel for two conditions makes the message say what the caller must then re-derive; two sentinels let the API word each refusal for what held it.
