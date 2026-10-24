---
title: "The word for everything an install manages is `fleet`"
date: 2026-08-14
status: "Accepted"
summary: "The totality-of-managed-things noun becomes **fleet**, everywhere at once (code identifiers, routes, docs prose, glossary, scope grammar, nav): a fleet of SYSTEMS, uniform units maintained at scale against a standard, stationed across the location tree. The old noun retires to the docs denylist. Ruled all-in rather than surface-only: two words for one referent is a permanent translation tax"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [ui](/architecture/ui/), [health](/architecture/health/), [identity and access](/architecture/identity-access/)
- **Decision:** the totality an install manages is a **fleet**: the systems are the fleet's
  units, the components staff them, and the location tree is the geography they are stationed
  across. The rename is all-in, in one sweep: Go identifiers (`FleetProjection`, `FleetView`),
  the view route (`/views/fleet`), the console surface and its nav entry, docs prose, the
  glossary, and the scope grammar (`fleet-wide`). The old noun joins the docs denylist; the
  decision log and build log keep it as the historical record they are.
- **Context:** the product's differentiation is the SYSTEM model: every competitor manages
  rooms or devices, and none can say "a fleet of systems" because none has one. A fleet is
  uniform units maintained at scale against a type, which is exactly a standard-conforming
  system: standard as airframe type, system as tail number, slots as parts, verdict as
  maintenance state, location as base. The old word was accurate and industry-anchored
  (UK/enterprise IT usage) but reads as property and governance rather than operation, and the
  surface being named is operated daily. Two costs are accepted with eyes open: in
  observability vocabulary a fleet often means the COLLECTOR fleet (Elastic Fleet, Datadog
  Fleet Automation), so the word will occasionally be misread as node management; and the noun
  names the units better than the buildings, which the location tree keeps first-class
  regardless. Surface-only renaming (a fresh label over the old domain word) was rejected as a
  permanent translation tax between nav, glossary, and API vocabulary.
- **Tracked under** epic [#630](https://github.com/hyperscaleav/omniglass/issues/630), ahead of
  slice [#633](https://github.com/hyperscaleav/omniglass/issues/633).
