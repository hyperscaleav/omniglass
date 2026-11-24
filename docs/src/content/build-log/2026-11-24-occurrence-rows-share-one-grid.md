---
title: "Event and log rows share one grid and clip a long badge (#2642)"
date: 2026-11-24
refs: [2642, 2437]
---

An event key such as `collection-failed` overflowed the 5.5rem badge slot, because `truncate` on a flex
badge does not clip its text, and an events list put its message at a different x than the logs list
beneath it, because only a log reserved a facility column. `OccurrenceRow` now lays both kinds on one
grid (time, an 8rem badge, the source where shown, then the message), so no row carries an empty slot and
the message starts at the same x in an events list and a logs list stacked on one page. A log's facility
rides inline before its message instead of in a column of its own. The badge label clips in an inner span
with the full key on hover, and `RuleLink` caps a long rule name so it cannot squeeze the message. Tests:
`OccurrenceList.test.tsx` (the shared grid, no empty cell, the facility inline, the clipped badge; red
before the change).
