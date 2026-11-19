---
title: "A type says what kind of thing an instance is, and a system type names a use"
date: 2026-11-16
status: "Accepted"
summary: "Type keeps one meaning across every *_type registry, the row that says what kind of thing an instance is, so no identifier changes; genus retires from operator surfaces; system type survives beside standard as kind against build, and names what a system is for, never the space it sits in"
refs: [2034, 2028, 2067]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [core entities](/architecture/core-entities/)
- **Decision:** (#2034.) **Type** means one thing: the registry row that says what kind of thing
  an instance is. A component is a `display`, a system is a `class`, a sample is `cpu_load`, an
  event is `link-down`; what differs is only whether the instance is an entity or a signal, and
  that is not a second meaning. Every `*_type` table, `/x-types` route, `x_type:*` permission and
  Go name stays. The console keeps the split it already has, on purpose: a classifier registry
  says "Component types", "System types", "Location types"; a signal registry says its bare noun
  (Metrics, Properties, Events, Commands, Notifications), since an operator speaks of the metric
  `cpu_load`, not its metric type. **Genus** retires from operator surfaces and joins the docs
  denylist; ADR titles and anchors keep it as history. **System type survives** beside standard,
  as kind against build, the way component type sits beside product: one estate holds ten signage
  standards under one `sign` type ([ADR-0096](/architecture/decisions/#adr-0096-the-system_type-name-returns-as-the-coarse-space-taxonomy)
  stands). It names what a system is **for** (meeting, teaching, signage), never the space it sits
  in: space is the location's word, and location types are the operator's. The seed's `room`
  node and its children's room-suffixed labels change accordingly. The rewording is #2067.
- **Context:** the issue read type as two meanings, a classifier (component, system, location,
  secret types) and a signal definition (metric, property, event, command, notification types),
  and asked whether system type survives now that a standard says what a system is built to.
  Evidence: 9 `/x-types` route families, 9 `x_type:*` permission families and about 2,400 Go
  identifiers share the one shape; docs say "location type" 71 times, "event type" 67, "command
  type" 34, "notification type" 28, "component type" 27, "metric type" 23, "property type" 20,
  "system type" 18, "secret type" 11; 15 console hints say genus. Considered: **definition** for
  the signal side (`metric_definition` and so on), which churns every one of those surfaces to
  split a meaning that is not split, and drops event type, the word CloudEvents and peers use;
  and **folding system type into standard**, already weighed and rejected by ADR-0096, since a
  standard fork is two ways to build the same kind of system and the grouping across them would
  be lost. The duplicate the issue named, a `meeting` system type labelled Meeting Room beside a
  room location type, is the system type describing a space; naming the use removes it without
  removing the classifier.
