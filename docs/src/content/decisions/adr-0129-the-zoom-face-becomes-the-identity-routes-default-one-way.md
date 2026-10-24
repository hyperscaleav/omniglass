---
title: "The zoom face becomes the identity routes' default; one way to look"
date: 2026-08-21
status: "Accepted"
summary: "The zoom/workspace/leaf render at the identity routes by default (`?zoom=1` tolerated, never written); the classic detail face survives at `?view=detail` only until edit-in-blade lands; the ruled target: one altitude rule, edit in blades, tables as a list-density toggle"
---

- **Date:** 2026-08-21 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** The fleet faces stop being a query-param variant: `/locations/{id}`,
  `/systems/{id}`, and `/components/{id}` render the zoom, workspace, and leaf by default,
  and `?zoom=1` is tolerated but no longer written. The classic detail face survives at
  `?view=detail` (and under a legacy `?edit=1`) solely because editing still lives there;
  it retires when edit-in-blade lands. The reconciliation's target, ruled with this: one
  altitude rule (locations drill, systems open full screen, components open in a blade
  that can expand to its route), every entity edits in its blade, flat tables survive as a
  list-density toggle for bulk work, and the parallel inventory faces go away.
- **Context:** ADR-0126 parked the zoom behind a param precisely so the table faces could
  stay alive until the medium was judged. Judged: the console had become two products (an
  inventory world that edits and a fleet world that observes), with browse, detail, and
  edit each existing twice. The architect ruled the merge on the #795 review; this entry
  flips the default (stage 1), and the remaining stages (entity blades, edit-in-blade,
  operate absorption, classic retirement) follow as their own body of work.
