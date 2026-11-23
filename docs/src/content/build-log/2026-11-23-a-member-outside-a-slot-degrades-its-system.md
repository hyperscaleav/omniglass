---
title: "A member outside a slot degrades its system (#2459)"
date: 2026-11-23
refs: [2459, 2472, 2115]
---

A component of a hand-built system could be in outage while its system and every place above it
read healthy. Under [ADR-2459](/architecture/decisions/#adr-2459-a-member-outside-a-slot-degrades-its-system-and-an-occupant-still-to-attach-reads-incomplete)
an open alarm on a member that fills no slot now makes its system degraded (never outage: slots keep
deciding that), and a slot whose occupants are still to attach (a driver product with no endpoint)
reads incomplete, so a system started from a template no longer reads healthy before anything is
wired.

The engine change is in `internal/health` (`SystemVerdict`, `SystemReason`, `Component.ToAttach`)
and the storage recompute: membership, endpoint, product and component-delete writes now recompute
the systems a component belongs to, and the boot reconciles systems recorded under the old rule. The
system health read gains `members` (every member's open alarms and whether it fills a slot),
`reason`, and a slot's `to_attach`. The console's system page and drawer list every member's alarm
through `AlarmActions`, state the reason beside the health, and name the members not in a slot with
the way to place them.
