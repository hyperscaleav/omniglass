---
title: "An operator acknowledges an alarm (#728)"
date: 2026-08-13
refs: [728]
form: bullet
---

- **An operator acknowledges an alarm (#728).** An alarm records that a human has seen it, and the
  console shows the queue nobody has looked at yet. The `alarm` row gains `acknowledged_at` and
  `acknowledged_by`, `POST /components/{name}/alarms/{id}:acknowledge` sets them behind the new
  **`alarm:acknowledge`** permission (seeded on `operator`), the list read gains an
  `unacknowledged` filter, and the component's Alarms panel gains the action plus an
  unacknowledged indicator.

  The shape of the whole slice comes from one fact read out of the code rather than assumed: an
  alarm's raised state belongs to its **condition** (the one-open invariant is per
  `(component, dedup_key)`, ADR-0075), while an acknowledgement is a fact about a **person**. So the
  acknowledgement is two nullable columns orthogonal to `cleared_at` rather than a `status` enum, and
  `AcknowledgeAlarm` is the one alarm write that does **not** recompute health: acknowledging is not
  fixing, and a transition recorded there would stamp a change at a moment when nothing about the
  estate moved.

  Three decisions were left open by the definition and ruled here, each recorded in
  [ADR-0109](/architecture/decisions/#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve).
  The permission spells its verb out, like every other seeded action, because a permission string is
  operator-facing. A second acknowledgement is **idempotent**, keeping the first person and the first
  time and writing no second audit row, decided in the database by a conditional
  `where acknowledged_at is null` rather than by a read-then-write. And `deploy` does **not** get the
  grant: an alarm resolves scope on the component tier, a location-scoped `deploy` grant reaches no
  component at all (#714), and the shape `deploy` is actually granted in is exactly that one, so the
  capability would have read as given and refused when used.

  **Snooze and resolve were refused rather than deferred.** Snooze suppresses notification and the
  notification registry is unbuilt (#618), so it would have been a column that lies; resolve is either
  the existing clear under a second name or an unspecified concept. The three-verb permission string
  that had sat in the test fixtures and in the identity-access page for most of a year turned out to
  be neither seeded nor enforced by anything, and the fixtures that carried it now use vocabulary no
  route will ever stamp.

  Two expectations moved. `internal/rbac/rbac_test.go` asserted the read floor against
  `alarm:ack`, a permission that did not exist; it now asserts the same floor against the real
  `alarm:acknowledge`. `internal/seed/seed_shrink_test.go` used `alarm:ack,snooze,resolve` as its
  retired grant and now uses `widget:frobnicate,polish`, since the mechanic under test is the
  upsert's authority and borrowing a real resource's vocabulary for it is what left phantom prior art
  in the repo in the first place. `TestALocationGrantOfDeployReachesOneTier` gained `alarm` in its
  matrix, so the `deploy` question resurfaces on its own if the cross-tier expansion (#10) lands.

  One divergence recorded and not fixed: the identity-access page's three-way status split says a
  target the caller can read but not act on is a **403**, and every scoped route in the platform
  answers **404** there, this one included. Fixing it for this route alone would have made it the
  only route that answers differently from its own siblings, so it is one shared refusal primitive
  in [#736](https://github.com/hyperscaleav/omniglass/issues/736) and the page now marks the branch
  as design.
