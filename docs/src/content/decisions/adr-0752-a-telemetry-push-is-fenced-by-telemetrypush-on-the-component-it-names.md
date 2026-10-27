---
title: "A telemetry push is fenced by telemetry:push on the component it names"
date: 2026-10-26
status: Accepted
summary: "`POST /telemetry:push` keeps its scope fence and refuses synchronously: the owner it names in the body is a component target, resolved through ADR-0116's split with `visible_set(P, telemetry, push)` as the action half and, because push is the stronger grant, the push scope itself as enough to see the target: a component inside the push scope is pushed to with no `component:read` needed, one outside both the push scope and the caller's read is a non-disclosing 404, and one readable but not pushable a truthful 403. One check per batch, never per sample. `telemetry` resolves on the component tier alone (the ADR-0117 precedent). This supersedes the 2026-08-27 ruling on #752 that the route was capability-shaped with no per-target walk: the shipped route had always walked scope, and removing the walk would have let a component-scoped integration push to every component"
refs: [752, 427, 1200]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [api](/architecture/api/)
- **Decision:** (#752, the architect's ruling, superseding the 2026-08-27 one recorded on the issue.)
  1. **A fence, not a capability.** `owner.ref` names an estate target, the same shape as the routes
     [ADR-0116](#adr-0116-a-refusal-picks-its-status-from-the-difference-between-two-scopes) covers,
     so the route resolves it through `ResolveActionTarget`: the action half is
     `visible_set(P, telemetry, push)`. Push is the stronger grant, so the push scope is also
     enough to see the target: the route first resolves with the push scope as both halves, and a
     push-only integration (no `component:read`) pushes to exactly the components it is granted and
     learns nothing about any other. Only a miss resolves again with the caller's own
     `component:read` as the read half, to choose its refusal: outside both is the non-disclosing
     404; readable but outside the push scope is a truthful 403, safe because only a caller who
     could read that row anyway can meet it.
  2. **Component tier alone.** `telemetry` leaves the exclusive-arc family in
     `scope.applicableKinds` and resolves on the component tier, for
     [ADR-0117](#adr-0117-an-actuation-is-fenced-by-the-permission-that-authorizes-it)'s reason: the
     route's only owner kind is a component, and a location- or system-tier root can never match a
     component's ancestor chain, so it reached nothing while telling `scope.Covers` otherwise. The
     day a push may name a system or a location owner (#422), that tier joins, with a test.
  3. **Once per batch.** A batch has one owner, so the fence is one resolve per accepted request (a
     refusal costs a second, to pick its status), never per sample, and the instance check ([ADR-0456](#adr-0456-an-instance-names-which-value-of-one-key-on-one-owner-a-row-is-and-an-owner-level-read-reduces-across-every-instance))
     is one read of the owner's endpoint names, made only when a sample names an instance. A
     query-count test pins both: a batch of forty-five observations issues exactly the statements a
     batch of three does.
  4. **The node lane is unchanged and no longer silent.** A node is never this route's caller: it
     ships on its own subject and stays confined at `ResolveTaskOwner`
     ([ADR-0037](#adr-0037-telemetry-is-a-protobuf-event-over-jetstream-with-an-inline-owner-confining-consumer)). A batch
     that confinement orphans is now recorded (#1200): a warning per node per window naming the node,
     the task and the samples dropped, the rest of the window counted rather than logged, and a
     running count on the node itself (`orphaned_batches` and `last_orphaned_at` on `GET /nodes/{name}`), so the fence cannot be told apart from a broken pipeline
     only by its silence.
- **Context:** The 2026-08-27 ruling recorded the route as capability-shaped: `telemetry:push` as a
  grant-class check, no per-target scope walk, and an unrelated owner landing as a recorded anomaly on
  the ingest lane rather than a disclosing 403. Its premise did not hold. Since #427 the route had
  resolved its owner under `visible_set(P, telemetry, push)`, and a test pinned a wide-read,
  narrow-push principal closed; executing the ruling would have removed that fence and let a
  component-scoped integration push to every component, the over-permit ADR-0117 closed for
  `command:issue`. The grant-class model is reserved for non-entity resources, and a push names an
  owner entity. And the anomaly path did not exist: the API lane is trusted by subject, so this route
  is the only fence on the write, and a node-lane drop was silent (#1200). A synchronous refusal is
  also what a caller is owed: a recorded anomaly would be "202, and good luck".
