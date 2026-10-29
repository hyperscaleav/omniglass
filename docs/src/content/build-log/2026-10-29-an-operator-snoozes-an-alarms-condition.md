---
title: "An operator snoozes an alarm's condition (#1380)"
date: 2026-10-29
refs: [1380, 1318]
---

The first slice of #1318 builds the snooze record. `alarm_snooze` holds one row per snoozed condition,
keyed `(component_id, dedup_key)` with `snoozed_until` and `snoozed_by`, the condition identity of
ADR-0075 rather than an alarm row, so a flapping condition's every re-raise reads the same snooze.
`POST /components/{name}/alarms/{id}:snooze` takes a `duration` from `1m` to `7d` (a Go duration or
whole days; outside the bound a 422 naming it) and sets or replaces the deadline, and `:unsnooze` ends
it early, a no-op 200 on a condition that is not snoozed. Both are gated by the new `alarm:snooze`,
seeded on `operator` only and resolved on the component tier from that permission through
`ResolveActionTarget`, exactly as `alarm:acknowledge` is, and both are audited on the alarm they name
with the deadline before and after, so the timeline shows them with no change to its read. Every alarm
read carries `snoozed_until` and `snoozed_by` from a scalar subquery on the snooze's primary key, so the
`UPDATE ... RETURNING` reads stay single statements. The CLI gains `omniglass component alarm snooze`
and `unsnooze`, generated. The alarms page replaces the snooze half of its "deliberately absent"
caution with a section on snooze as a fact about a condition, its cycle-safety citation now points at
the page's own rule 2 rather than ADR-0069, and
[ADR-1318](/architecture/decisions/#adr-1318-a-snooze-is-a-fact-about-an-alarms-condition-its-notifications-are-written-suppressed-and-its-end-tells-people-again)
records the decision, amending ADR-0109.
