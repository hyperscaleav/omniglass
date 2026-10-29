---
title: "A snooze is a fact about an alarm's condition: its notifications are written suppressed, and its end tells people again"
date: 2026-10-29
status: Accepted
summary: "A snooze is one `alarm_snooze` row per condition (`component`, `dedup_key`) with a deadline (1m to 7d), set and ended from any alarm of that condition by `:snooze` and `:unsnooze` under `alarm:snooze` (component tier, on `operator`). While it holds, every row a firing rule would write is written `suppressed` with the reason, on every channel, a raise and a clear alike, and a row already waiting out a retry is marked `suppressed` by the tick. When it ends with an alarm of the condition still raised, that alarm is announced again as `raised` against the rules as they stand: the one operator act that queues notifications. The deadline sweep runs on the notifier tick until #419. Refines ADR-0109's refusal of snooze and the rule that operator transitions do not notify"
refs: [1318, 109, 618, 1341, 419, 1345]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [identity and access](/architecture/identity-access/), [API](/architecture/api/), [UI](/architecture/ui/)
- **Decision:** Snooze is built (#1318, its open decisions approved as recommended):
  1. **It attaches to the condition.** One `alarm_snooze` row per `(component_id, dedup_key)`, the
     condition identity of [ADR-0075](#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key),
     with `snoozed_until` and `snoozed_by`. It is set from any alarm of the condition, cleared ones
     included, for a duration from now of 1 minute to 7 days; snoozing again replaces the deadline.
     `alarm:snooze` gates both verbs, its scope resolved on the component tier from that permission as
     `alarm:acknowledge`'s is, and it is seeded on `operator` and not on `deploy` (ADR-0109's reason:
     a location-scoped grant reaches no component) or `viewer`. Both verbs are audited on the alarm
     they were called on.
  2. **Suppression is a delivery status.** `notification.status` gains a fourth, terminal value,
     `suppressed`, the reason in `last_error`. At the queue seam ([ADR-0618](#adr-0618-notifications-are-postgres-rows-drained-by-a-server-tick-not-cdc-to-a-jetstream-sender)
     point 1) one indexed lookup of the alarm's condition runs once some rule fires, and while the
     condition is snoozed every row the firing rules would write (webhook, email, inbox, a group's
     people, a personal rule's owner) is written `suppressed` instead of `queued` or `sent`. A raise
     and a clear are both suppressed. A rule whose `when` fails still writes `failed`, so a broken
     rule stays visible. The tick never drains a suppressed row, the inbox never shows one, and
     `:retry` refuses one. Where a row waits (the pattern of [ADR-1341](#adr-1341-a-person-is-told-about-an-alarm-only-while-they-can-read-it-groups-expand-at-queue-time-scope-is-checked-there-and-again-at-delivery)
     point 3), the tick marks a queued row of a snoozed condition `suppressed` instead of sending it,
     so a snooze also stops a retry already on its way. This is the primitive #1345's dependency
     suppression and dedupe consume.
  3. **The end of a snooze tells people again about a condition that is still raised.** When the
     deadline passes, or `:unsnooze` ends it, the row is deleted and, if an alarm of the condition is
     still open, it is announced again as a `raised` transition, judged against the action rules and
     memberships as they stand then (a rule disabled during the snooze stays quiet; ADR-1341's
     queue-time scope check filters the people, unchanged). The end is audited: `unsnooze` on the
     alarm it was called on, naming the actor, or `snooze-end` from the platform on the condition's
     latest alarm. A suppressed row is
     never released: the rules are judged again from scratch, so nobody gets a backlog of stale
     messages.
  4. **The deadline sweep runs on the notifier's 2s tick.** Each expired snooze is taken
     `FOR UPDATE SKIP LOCKED` and deleted in the same transaction that queues its announcement, so
     replicas never announce one condition twice and a restart loses nothing. It moves to the time
     primitive when #419 lands, as ADR-0618 records for the drain.
- **Refines** [ADR-0109](#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve):
  its refusal of snooze rested on there being nothing to suppress, and #618 built the notification
  lane. Un-acknowledge, resolve and bulk acknowledge stay refused. It also refines the alarms page's
  cycle-safety rule 2 (operator transitions do not match action rules): snoozing queues nothing, and
  the end of a snooze is the one operator act that queues notifications. It is safe because the
  announcement is an alarm transition the platform already sent once, judged by the same rules, and
  a snooze can end only once.
- **Context:** A snooze on one alarm row could only ever hold back that row's clear, since its raise
  has gone out by the time an operator sees it, so it would do nothing for the flapping alarm the
  opt-out docs point to snooze for; a rule-wide silence already exists as `action_rule.enabled`.
  Announcing again at the end matches "leave me alone for an hour, then tell me again", needs no
  record of which raise was suppressed, and does not hide an outage that began during the snooze.
  Muting only your own delivery is a different record (a person and a condition), filed as #1384.
