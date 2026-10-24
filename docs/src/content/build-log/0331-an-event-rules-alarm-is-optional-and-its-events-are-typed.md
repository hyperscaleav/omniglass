---
title: "An event rule's alarm is optional, and its events are typed (#938, slice #948)"
date: 2026-10-20
refs: [938, 948]
---

The first slice of the event-to-alarm edge. `event_rule.severity` is nullable (the reference to the
level registry keeps `ON UPDATE CASCADE`): a rule with no severity is informational, and the evaluator
raises no alarm for it, while a clear still resolves an alarm the rule holds open from before it went
informational. A rule names its event types from the `event_type` registry by name, a required
`fire_event_type` and an optional `clear_event_type`, stored as `fire_event_type_id` and
`clear_event_type_id` references (a type a rule names cannot be deleted; deleting a referenced event
type is now a 409, where it was a 500). The boot seed lands two official types, `condition-started`
and `condition-ended`, and gives the official `reachability-lost` rule that pair; the seeded-rules table
on the rules guide renders the types and an informational rule's absent severity from the seed. The
schema change is three migrations, one per bucket: the DDL (the nullable severity, the two type
references, `event.alarm_id` with `ON DELETE SET NULL`, and the `event_rule_condition` table keyed and
cascading like `event_rule_state`), a one-time backfill giving every existing rule the generic pair
(creating the two types insert-if-absent when rules exist, since the boot seed runs after migrations;
a fresh database gets them from the seed alone), and the `NOT NULL` on the fire type after it.
`/event-rules` takes and returns the new fields (`severity` absent on read for an informational rule;
a PATCH with an empty `severity` makes a rule informational, an empty `clear_event_type` drops the
clear event, an empty fire type is a 422), and the CLI follows from `make gen`. Changing a rule's event
types is not a material edit: it changes what the rule writes at an edge, not when the edge falls, so
its streaks keep running. The console's new-rule form sends the generic pair until it gains the type
pickers (#951). Writing the events at the edge, the condition rows and `event.alarm_id` is the next
slice (#949); the page badges move with the epic's last slice.
