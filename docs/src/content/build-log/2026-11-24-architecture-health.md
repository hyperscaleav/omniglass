---
title: "The Architecture section gains health and automation: health, alarms, calculations, expressions, templates and time (#2320)"
date: 2026-11-24
refs: [2320, 2153]
---

Six Architecture pages are rewritten fresh from the vocabulary and checked against the code, each
badged `Partial` and listed in the live section: Health, Alarms and actions, Calculations, Expressions,
Templates and Time. Four of them were design sketches whose capability has largely shipped, so they
leave the sketch list with the floor the code has reached. Every unbuilt part sits in a fence naming the
open issue that tracks it; four had none, and are now #2607 (service levels and KPIs), #2608 (unknown
and staleness in the health rollup), #2609 (unit conversion and divergence functions) and #2610 (trust
for templates from outside the binary).

The rewrite corrects drift the old pages carried: a calc rule's unchanged metric output is written again
once it is 12 hours old, so a steady value stays inside retention, and Time now describes the server's
built loops (the sweep, the reconcile, the calc and notification ticks, the ages that make silence an
alarm) rather than only the clock still to come. Design that a decision record already diverged from
(a change-data-capture publisher feeding JetStream consumers for calc rules and notifications) is
dropped rather than fenced. Calculations becomes an MDX page so its storage tables render from the
generated schema. Every page keeps its URL and the anchors other pages link to.
