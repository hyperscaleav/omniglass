---
title: "A system shows the alarms of members outside its slots (#2459)"
date: 2026-11-23
refs: [2459, 2499]
---

A component of a hand-built system could be in outage while its system's page listed no alarm and
read healthy. Under [ADR-2459](/architecture/decisions/#adr-2459-an-alarm-on-a-member-outside-a-slot-is-shown-on-its-system-not-rolled-up)
the system now shows it: the system health read carries every member with its open alarms and
whether it fills a slot, plus a reason line naming the slots behind the verdict, and the shared
system detail body (page and drawer) lists every member's alarm through `AlarmActions` with
"impairs X", "in X" or "not in a slot", states the reason beside the health, and names the members not
in a slot with the way to place them. Health itself is unchanged: it rolls up through slots alone, so
such an alarm moves nothing above its component. A rule that degraded the system on any such alarm was
built in review and withdrawn.
