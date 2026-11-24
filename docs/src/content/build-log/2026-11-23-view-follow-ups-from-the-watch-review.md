---
title: "Log tail ids pair by construction; view limits derive from constants (#2258)"
date: 2026-11-23
refs: [2258, 2056]
---

`writeLogLines` no longer trusts the sequence to draw ids in the insert's row order: like `InsertEvents`,
it draws each id beside its ordinal and inserts it, so a log line reaches the tail under its own
component. The `/views:run` and `/views:watch` descriptions are built from the constants they restate,
a test holds the struct-tag maxima to those constants, and the views and roadmap pages and ADR-1830 now
say a `reset` names one or more bindings, the hub opens one watch per 32 bindings, and the view binding
has shipped.
