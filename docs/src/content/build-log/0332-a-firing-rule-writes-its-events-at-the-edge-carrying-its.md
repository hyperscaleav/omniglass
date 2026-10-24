---
title: "A firing rule writes its events at the edge, carrying its alarm (#938, slice #949)"
date: 2026-10-20
refs: [938, 949]
---

The second slice of the event-to-alarm edge. A rule's in-force state is now its own
`event_rule_condition` row, for every rule, informational or not, rather than the alarm it holds open.
Every path that judges a rule reads it: the ingest evaluator's in-force check, the sweep's re-judge
before it acts, priming on create, enable or material edit, and the #928 reconcile pass, whose drift
check reads the condition in the same one-statement scan (so an informational condition held across
samples no longer reads as drifted on every pass, and its streaks no longer churn). When a condition
comes into force the rule writes the row, opens its alarm when it has a severity, and writes its fire
event; when it leaves, it deletes the row, resolves the alarm and writes its clear event when it has a
clear type: once per edge, in the transaction that moves the alarm and recomputes health. The event is
`origin = derived` on the series' component and instance, with the rule's message and the sample's
key and value as attributes, `derived_by_rule_id` (now a reference to `event_rule`, set null with the
rule, by a new migration), the rule's name and version in `source_rule` and `source_rule_version`, and
`alarm_id` set to the alarm it opened or resolved. Disabling, deleting or narrowing a rule ends its
conditions with their clear events; turning a severity on opens alarms for the conditions in force,
and turning it off resolves them, leaving the conditions in force, with no condition event either way
(the alarm's audit rows, naming the editor, record a rule edit's open and resolve), and a material
edit primes before its severity reaches the conditions. A hand clear of a rule's alarm holds until the
condition ends and returns, and an alarm the rule holds open with no condition row (an older binary's,
mid-upgrade) counts as in force, so a recovery resolves it rather than stranding it. A one-time backfill gives every rule-raised alarm open
at upgrade its condition row, since it opened, so the next recovery resolves it with a clear event. The
component and system event reads return `derived_by_rule` and `alarm_id`. The netadmin end-to-end
test now proves, on the real stack, that each alarm the unplug opens was opened by exactly one
`condition-started` event carrying its id, and resolved after the replug by exactly one
`condition-ended` event carrying it.
