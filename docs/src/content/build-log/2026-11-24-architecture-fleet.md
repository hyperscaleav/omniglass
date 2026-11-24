---
title: "The Architecture section gains the fleet model: core entities, data model, cascade, variables, properties and commands (#2320)"
date: 2026-11-24
refs: [2320, 2153]
---

Six more Architecture pages are rewritten fresh from the vocabulary and checked against the schema and
routes: Core entities, Data model, Cascade, Variables, secrets and declared values, Metrics and
properties, and Commands. The data model is the generated ERD and is badged `Built`; the other five
are `Partial`, each unbuilt part fenced with the issue that tracks it. Three gaps had no issue and now
do: a unit catalog (#2613), fusing several sources of one key (#2614), and a secret's rotation,
expiry and validity health (#2615). Fences that named the closed telemetry epic #430 now name these.

The rewrite corrects drift the old pages carried: a location's type is required, not optional; the
`disagree` operator is unbuilt (#2609), not a working criteria function; and the `$var:` and `$sec:`
tokens are parsed but spliced by no consumer yet. The pages keep their URLs and every heading another
page links to, and leave the Being rebuilt sidebar group.
