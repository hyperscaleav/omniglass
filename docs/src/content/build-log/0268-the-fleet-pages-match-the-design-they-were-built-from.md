---
title: "The fleet pages match the design they were built from"
date: 2026-08-19
refs: []
form: bullet
---

- **The fleet pages match the design they were built from**
  ([#630](https://github.com/hyperscaleav/omniglass/issues/630)). A fidelity pass after the
  architect compared the shipped zooms with the approved prototype: the right rail lands on
  every zoom as one primitive (`ZoomRail` over a pure `railModel`: headline, a per-verdict
  ratio bar over the components in scope, the location types present, a worst-first attention
  list, the gaps footer); the location zoom's system cards become the design's card (status
  border, room and standard, a slot strip computed over the active roles' quorum with the gap
  line in the design's words); the ladder is a stepper with an arrow between levels and a
  per-zoom hint; the inert `+ Top-level location`, `+ Location` and `+ System` cards mark
  where a thing would go, per the epic's ruling that mutation renders as a hole; the leaf gains
  the what-it-is and where-it-sits panels, the type path, and the sample age; the dot field
  densifies. The dev fleet grows to a size the canvas can be judged at (57 locations, 41
  systems, 112 components). One wire change rode along: a membership now carries both ends'
  uuids beside their names (`system_id`, `component_id`), because a shared component's
  memberships can legally name two systems that share a bare name, and a console that could
  only address by name either guessed or refused (#645's class, now closed for this read).
