---
title: "Severity levels are authored on the console (#930)"
date: 2026-10-19
refs: [930]
---

**Catalog > Actions > Severity** went from a read view to a registry an operator writes: **New severity
level** and a level's **Edit** open one form in a drawer (name, label, color, rank), and **Delete** sits
behind a confirm; official levels stay read-only. Writes use the existing API and gate on
`severity_level:*` on an all-scope grant, like a rule write (ruled 2026-10-20, ADR-0144): a level's rank
moves health fleet-wide, so the gateway refuses a bounded holder (`ErrSeverityLevelForbidden`, a 403,
proved by a testcontainer storage test and an API test of a place-scoped owner), and the console gates
New, Edit and Delete on the caller's all-scope permissions (`canFleet`). The form
teaches the rank as it is typed, against the live registry: where the rank sits ("Between Average (30)
and High (40)."), whether an alarm at it takes its component out or degrades it, and the chip its
alarms will wear; a name or rank another level holds is refused before anything is sent, and a delete
the server refuses (a level an alarm or rule still names) says why on the blade.
