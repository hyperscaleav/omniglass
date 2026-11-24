---
title: "Event and log rows share one grid and clip a long badge (#2642)"
date: 2026-11-24
refs: [2642, 2437]
---

An event key such as `collection-failed` overflowed the 5.5rem badge slot, because `truncate` on a flex
badge does not clip its text, and an events list put its message at a different x than the logs list
beneath it, because only a log reserved a facility column. `OccurrenceRow` now lays both kinds on one
grid (time, a 9rem badge, the source where shown, a facility slot an event leaves empty, then the
message), truncates the badge label in an inner span with the full key on hover, and `RuleLink` caps a
long rule name so it cannot squeeze the message. Tests: `OccurrenceList.test.tsx` (the shared grid and
the clipped badge, red before the change).
