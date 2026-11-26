---
title: "A behavior tick streams only what it changed, and may report named events"
date: 2026-11-26
status: Accepted
summary: "A simulator behavior's tick counts as a change only the paths whose value it moved: a write of the value a path already holds publishes no diff, bumps no version and puts nothing on the device's feeds. A behavior may also report named events after its tick, rendered against the state it left, and one whose data renders empty is not reported that tick."
refs: [2754, 1145, 1360]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** A behavior tick applies its mutations as before, but its diff is only the paths whose
  value changed (numbers compared as numbers, so a profile's YAML integer and a template's integer are
  one value); an unchanged write publishes no state change, bumps no version and fans out to no events,
  websocket or session face. Routes, commands and overrides keep reporting every write. A behavior may
  declare `events:`, as a route does (ADR-1145): each renders against the state the tick left, in the
  same numbered stream as the changes, and an event whose data renders empty (white space only) is
  skipped that tick, so the template's own condition says when it fires.
- **Context:** The video bar's meeting schedule (#2754) is a timer that rewrites the call's watched
  paths every tick; with every write a change, the bar's feed would stream its call state and
  participant count every minute and the driver would land a property sample each time, against
  #1360's cost rule (properties transition-only, richness from more kinds of signal). A tick that
  reports only movement keeps the feed a record of transitions. Named events on a behavior are what let
  a device report an occurrence nobody requested (a call starting on schedule), where a route can only
  report one a request caused.
