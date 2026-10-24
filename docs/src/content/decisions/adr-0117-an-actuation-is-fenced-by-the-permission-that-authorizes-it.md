---
title: "An actuation is fenced by the permission that authorizes it"
date: 2026-08-14
status: "Accepted"
summary: "`POST /components/{name}/commands:issue` fences its write with `visible_set(P, command, issue)` against the caller's own `component:read`, through ADR-0116's split. It used to resolve all three of its gateway calls with `component:read`, so a principal holding `viewer @ all` beside a room-scoped operator grant could command **every component in the estate**: not a wrong refusal but a wrong REQUEST, since a command records an intent a driver acts on. The load-bearing half is that **`command` becomes a component-tier resource**: it was absent from `applicableKinds`, where every scoped grant resolves to the empty set, so fencing on that set without registering it would have denied every scoped issuer instead of fencing them. The **component tier alone**, not the arc the command table's owner columns allow, because the only route that issues addresses a component and a location- or system-tier root can never match a component's ancestor chain. A `command:issue` grant at those tiers now reaches nothing, the same tier rule `alarm:acknowledge` lives under ([ADR-0109](#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve), #714), and the anomaly was that it ever commanded. The target is resolved **once and bound by id**, so `IssueCommand` and `CommandSettlement` take no scope: a split cannot be applied from one parameter, and two of the route's three name resolves go with it"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [commands](/architecture/commands/)
- **Decision:** `POST /components/{name}/commands:issue` resolves its target with
  `visible_set(P, command, issue)` as the action half and the caller's own `component:read` as the
  read half, through the same `ResolveActionTarget` split as the acknowledgement
  ([ADR-0116](#adr-0116-a-refusal-picks-its-status-from-the-difference-between-two-scopes)). It used
  to resolve all three of its gateway calls with `component:read`, so the set that fenced the write
  was the caller's read set and the issue grant narrowed nothing.
- **Why this one is not a status question.** Every route ADR-0116 touched was answering the wrong
  REFUSAL. This one was answering the wrong REQUEST: a principal holding `viewer @ all` beside a
  room-scoped operator grant could issue commands to every component in the estate, and a command
  records an intent a driver acts on. The blast radius is a device doing something, not a row being
  named. That is why it shipped on its own rather than inside the sweep.
- **`command` becomes a component-tier resource, and that is the load-bearing half.** The resource
  was not in the scope resolver's `applicableKinds` at all, so it fell to the default: only an
  all-scoped grant resolved to anything, and every scoped grant resolved to the empty set. Fencing
  on that set without registering the resource would have denied every scoped issuer rather than
  fencing them, which is a worse outcome than the bug. It joins the component tier beside `alarm`,
  for the same reason and with the same shape: a command is issued TO a component, and it resolves
  from grants carrying `command:issue` so that a wide component read cannot widen what may be
  commanded, and a component-update grant is not what decides who may command.
- **The component tier alone, not the three-tier arc.** The `command` table's owner columns allow
  any exclusive-arc owner, but the only route that issues one addresses a component. Admitting
  `location` or `system` here would put roots in the set that a component's own ancestor chain can
  never match (the tier mismatch `resolvePlacementRef` records) while telling `scope.Covers` that a
  command scope may be checked against those tables. A route that issues to another tier adds that
  tier, with a test.
- **Consequence, stated rather than discovered later.** A principal whose only `command:issue`
  grant sits at the location or system tier can no longer issue anything. That is the same tier rule
  `alarm:acknowledge` already lives under ([ADR-0109](#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve),
  [#714](https://github.com/hyperscaleav/omniglass/issues/714)): such a grant reaches no component,
  so it could not update, read, or acknowledge one either, and being able to command one was the
  anomaly. It resolves when a grant's scope can span tiers, not before.
- **Resolved once, bound by id.** `IssueCommand` and `CommandSettlement` take the resolved id and
  **no scope**. The fence on an actuation is a read-versus-issue split rather than a single set, so
  it cannot be applied from one parameter inside a gateway write; a set re-checked there on an
  already-resolved row could only re-answer the settled question or disagree with it. It also
  removes two of the route's three name resolves, and a path with one resolve cannot have a second
  that lands on a different row (ruling 2, [#627](https://github.com/hyperscaleav/omniglass/issues/627)).
  The scope check moves out of the write's transaction, which is the one thing given up: at READ
  COMMITTED an in-transaction guard never closed that window either, it only narrowed it.
- **Proved in the conformance matrix, not beside it**, so the `mixed` principal (readable, outside
  the action scope) drives the route automatically. The RED was not a wrong status: it was a **200
  with a recorded invocation** on a component outside the caller's issue scope.
