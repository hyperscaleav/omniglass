---
title: "An alarm carries an acknowledgement, and not a snooze or a resolve"
date: 2026-08-13
status: "Accepted"
summary: "An alarm's raised state belongs to its **condition** (ADR-0075) and an acknowledgement is a fact about a **person**, so the acknowledgement is two nullable columns orthogonal to `cleared_at`, never a `status` enum, and `AcknowledgeAlarm` is the one alarm write that does **not** recompute health: acknowledging is not fixing. **Snooze** and **resolve** were refused rather than deferred: snooze suppresses notification and the notification registry is unbuilt (#618), so it would be a column that lies, and resolve is either the existing clear under a second name or an unspecified concept. The permission is **`alarm:acknowledge`**, spelled out like every other seeded verb; the `alarm:ack,snooze,resolve` string that appeared in test fixtures and in the identity-access page was never a design and nothing ever seeded or enforced it. Its scope resolves on the component tier from `alarm:acknowledge` itself rather than from `component:update`, and it is granted to `operator` and **not** to `deploy`, because a location-scoped `deploy` grant reaches no component tier at all (#714) and would hold a capability that acknowledges nothing. A second acknowledgement is **idempotent**, keeping the first person and the first time and writing no second audit row"
---

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [identity and access](/architecture/identity-access/), [API](/architecture/api/)
- **Context:** `alarm:ack,snooze,resolve` had appeared for most of a year in
  `internal/seed/seed_shrink_test.go`, in `internal/rbac/rbac_test.go`, and in the permission-format
  fence and the worked example on the identity-access page. Nothing seeded it, no route registered
  it, and no design document ever specified what snooze or resolve would do. It read as prior art,
  which is exactly the risk of borrowing a real resource's vocabulary for a fixture: three verbs
  looked decided, and only one of them had a meaning.
- **Decision (what is built):** the acknowledgement, alone. Two nullable columns on `alarm`,
  `acknowledged_at` and `acknowledged_by`, plus
  `POST /components/{name}/alarms/{id}:acknowledge`, the `alarm:acknowledge` permission, the seeded
  grant on `operator`, an `unacknowledged` filter on the list read, and the console affordance.
- **Decision (orthogonal, not a status):** an alarm's raised state belongs to its **condition**: the
  one-open invariant is per `(component, dedup_key)` and the raiser owns the key
  ([ADR-0075](#adr-0075-an-alarm-carries-its-condition-identity)). An acknowledgement is a fact about
  a **person**. Two independent facts, therefore two nullable columns and not a `status` enum, which
  would force them into one column and make acknowledged and cleared mutually exclusive. All four
  combinations are real and each one is tested: raised and unacknowledged (the queue), raised and
  acknowledged (somebody is on it), cleared having never been acknowledged (it came and went), and
  cleared after being acknowledged. A cleared alarm stays acknowledgeable, because the orthogonality
  runs in both directions and the record of who read an incident outlives the fix exactly as the row
  does.
- **Decision (no health recompute):** raising and clearing both recompute the health chain in their
  own transaction, and acknowledging deliberately does not. Acknowledging is not fixing: a transition
  recorded here would stamp a change at a moment when nothing about the estate moved, and the health
  history is meant to be edges and only edges.
- **Decision (the permission's name):** **`alarm:acknowledge`**. Every other seeded action spells its
  verb (`create`, `update`, `rename`, `move`, `reveal`, `issue`, `push`, `purge`,
  `revoke-session`), and a permission string is operator-facing: it is rendered in the Roles view, in
  the grant builder's tooltips, and in `/auth/me`. `alarm:ack` would have been the only abbreviation
  in the set, and its only claim was the fixture this ADR exists to disown.
- **Decision (idempotent, not refused):** a second acknowledgement of the same alarm keeps the first
  person and the first time, returns 200, and writes no second audit row. The recorded fact is
  monotonic, and the first sighting is the one that means something operationally (time to
  acknowledge). Refusing would turn a double click into an error an operator has to interpret, and
  would make two people opening the same queue race into a 409 that teaches nothing. It is the same
  shape the raise path already has: a re-raise of an open condition returns the existing incident
  and writes no audit row, because the no-op leg changed nothing. The database decides it, through a
  conditional `where acknowledged_at is null`, rather than a read-then-write that a concurrent
  acknowledgement could lose.
- **Decision (the scope, and why `deploy` does not get it):** the acknowledgement resolves
  `visible_set(P, alarm, acknowledge)` from **its own permission** on the **component** tier, not
  from `component:update` and not from `component:read`. A responder role may acknowledge without
  editing components, and a principal commonly holds an estate-wide read beside a narrow write, so
  binding the scope to either neighbour answers the wrong question in one direction or the other.
  Given that, `deploy` does **not** get the grant: a `deploy` grant is made at a **location** in
  practice (devseed's `tech-east` is exactly that shape), and a location-kind grant fills no
  component-tier scope at all ([#714](https://github.com/hyperscaleav/omniglass/issues/714)). The
  capability would resolve to nothing while the console's capability-only hint still offered the
  button: a grant that reads as given and refuses when used. It is revisitable when the cross-tier
  expansion ([#10](https://github.com/hyperscaleav/omniglass/issues/10)) lands, and
  `TestALocationGrantOfDeployReachesOneTier` now carries `alarm` in its matrix so the question
  resurfaces on its own.
- **What was refused:** **snooze**, because its purpose is to suppress **notification** and the
  outbound notification registry is unbuilt
  ([#618](https://github.com/hyperscaleav/omniglass/issues/618)); a snooze with nothing to suppress
  is a column that lies about what it does, and it belongs with that issue or after it. **Resolve**,
  because the platform already has `DELETE /components/{name}/alarms/{id}`, so a human "resolving"
  an alarm whose raised state belongs to its condition is either that clear under a second name or a
  different concept nobody has specified. **Un-acknowledge**, which is a second verb and a second
  decision rather than a nullable field somebody flips, and has no evidence behind it yet. **Bulk
  acknowledge**, until there is evidence the queue needs it. And **a denormalized acknowledger
  label** on the alarm row: `audit_log` already denormalizes the actor's name at write time for
  exactly the purge case, so the alarm keeps the foreign key (`ON DELETE SET NULL`) and resolves the
  current label on read, which reads honestly empty after a purge while the durable record survives
  where it already lived.
- **Divergence recorded:** the identity-access page's three-way status split says a target the
  caller can **read** but not act on is a **403**. Every scoped route in the platform answers
  **404** there, this one included, because the target is resolved through the gateway with the
  action's own scope and a miss is a miss. Making it three-way is one shared refusal primitive
  across every scoped route rather than a per-route special case, and doing it for this route alone
  would have made it the only route in the system that answers differently from its own siblings.
  Tracked in [#736](https://github.com/hyperscaleav/omniglass/issues/736); the page now marks the
  branch as design.
- **Tracked under** [#728](https://github.com/hyperscaleav/omniglass/issues/728),
  [#733](https://github.com/hyperscaleav/omniglass/issues/733),
  [#734](https://github.com/hyperscaleav/omniglass/issues/734) and
  [#735](https://github.com/hyperscaleav/omniglass/issues/735).
