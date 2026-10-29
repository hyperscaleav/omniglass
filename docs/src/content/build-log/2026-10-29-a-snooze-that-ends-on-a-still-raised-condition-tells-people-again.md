---
title: "A snooze that ends on a still-raised condition tells people again (#1382)"
date: 2026-10-29
refs: [1382, 1318]
---

The third slice of #1318 closes the snooze's loop. `EndExpiredSnoozes` is the deadline sweep: it takes
each snooze past its deadline `FOR UPDATE SKIP LOCKED`, one per transaction, deletes it, audits
`snooze-end` from the platform on the condition's latest alarm and, if an alarm of the condition is
still open, announces it again as a `raised` transition through the one queue seam, judged against
the rules and memberships as they stand then. `:unsnooze` takes the same path, audited as `unsnooze`
naming the operator. The notifier's 2s tick runs the sweep before each drain, so an announcement is
delivered in the same pass, and a sweep that fails is logged without stalling the drain; it moves to
the time primitive when #419 lands. Two gateways sweeping one database end each snooze once, a rule
disabled during the snooze stays quiet, and a condition that cleared during it gets nothing.
ADR-1318 records the end as the one operator act that queues notifications, and the alarms page's
cycle-safety rule 2 and the workers page say so.
