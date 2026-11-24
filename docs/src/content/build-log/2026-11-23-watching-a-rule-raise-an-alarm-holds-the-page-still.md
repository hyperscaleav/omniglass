---
title: "Watching a rule raise an alarm holds the page still (#2006)"
date: 2026-11-23
refs: [2006, 1461]
---

The `watch-a-rule-raise-an-alarm` walkthrough scored over the 0.1 layout-shift budget from two
shifts the operator did not cause. The component page keyed its reads on the route param, so the
name-to-uuid redirect changed every key, sent the sections back to loading and collapsed two of them;
they now key on the resolved component id, so the redirect refetches nothing. The system page
mounted the "Why" strip only when an alarm opened, pushing the body (and the map) down; the slot is
now held while quiet, with a "No active alarms" line the same height as a one-alarm strip.
Regression tests: `does not refetch its reads when a name address resolves to the uuid`
(`ComponentLeaf.test.tsx`) and the quiet-slot assertions in `SystemZoom.test.tsx`. The probe
score over three runs was not measured here (no lab in this run).
