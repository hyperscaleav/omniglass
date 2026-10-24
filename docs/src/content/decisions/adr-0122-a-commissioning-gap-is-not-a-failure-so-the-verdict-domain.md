---
title: "A commissioning gap is not a failure, so the verdict domain gains `incomplete`"
date: 2026-08-09
status: "Accepted"
summary: "The verdict domain gains a fourth value: a role short because its hardware was never installed reads `incomplete` (ranked between healthy and degraded) rather than its declared `impact`, which now describes failure only; `impact: none` stays healthy when empty and a losing alternate's roles contribute nothing. No migration. Amends ADR-0050"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [health](/architecture/health/),
  [glossary](/architecture/glossary/)
- **Decision:** the verdict domain becomes **`healthy` < `incomplete` < `degraded` < `outage`**.
  A role short of quorum because its assigned hardware is **failing** contributes its declared
  `impact`, unchanged; a role short because the hardware was **never installed** contributes
  `incomplete`. A role that is both reads its `impact`, the worse of the two by rank. A role
  declaring `impact: none` reads `healthy` when empty rather than `incomplete`, because a slot
  whose failure does not matter has an absence that does not matter either. A role inside a
  choice whose alternate did not win contributes nothing at all, `incomplete` included. No
  migration: the enum lives in `internal/health` and the value persists as a string, so the only
  storage-side change is the `health` property type's seeded `validation.enum`, which is boot-seed
  reference data. Amends [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)'s
  three-value domain; its transition-only recording, pure-package judgement, and worst-wins rollup
  stand unrevised.
- **Context:** the estate canvas ([#630](https://github.com/hyperscaleav/omniglass/issues/630))
  paints one dot per component across the whole estate, and it was the surface that made the
  three-value domain untenable. Most of a real estate is mid-commissioning for months, and under
  the old rule every unstaffed role contributed its declared `impact`, so a half-built site
  rendered as a wall of red indistinguishable from a site on fire. A colour an operator learns to
  ignore is worse than no colour. Ranking the new value between `healthy` and `degraded` is what
  makes it useful in both directions: a gap is visible above a finished room and invisible beneath
  anything actually broken, so neither reading is drowned by the other. The `impact: none` carve-out
  came out of the existing suite rather than the design: `TestImpactMapping` and
  `TestSystemVerdictWorstWins` both asserted that an unstaffed harmless role stays harmless, and
  they were right, since reporting a gap there would put a permanent `incomplete` on every
  confidence monitor nobody ever intends to staff, reintroducing the saturation this decision
  exists to remove. Rejected: a separate non-health field for commissioning state (two axes to
  roll up, two badges to reconcile, and the estate view would have to render both to answer one
  question); and reusing `unknown`, which already means "no health has been read" and would
  conflate never-measured with never-installed.
- **Tracked under** epic [#630](https://github.com/hyperscaleav/omniglass/issues/630), slice
  [#631](https://github.com/hyperscaleav/omniglass/issues/631).
