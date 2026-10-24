---
title: "A read-only field renders as a fact, not as a box that refuses typing"
date: 2026-08-04
status: "Accepted"
summary: "a blade the operator cannot edit contains nothing shaped like a control; BladeField owns the read-or-edit switch"
---

- **Date:** 2026-08-04 | **Status:** Accepted | **Pages:** [UI and the design system](/contributing/design-system/)
- **Decision:** a field that is not being edited renders as a **fact** (`KVStacked`: an eyebrow label
  above a value), never as a bordered control. `BladeField` owns the read-or-edit switch, the
  read-only treatment, the free-text shape, and the identity label pairing, so each is decided once
  instead of at every field. A blade the operator cannot edit therefore contains no element shaped
  like a control.
- **Context:** the blade shell was a primitive and the blade contents were not. Eleven pages defined
  a byte-identical local `Field`, four more went through positional `ctx.field(...)` / `ctx.fact(...)`
  helpers, and the read-only rendering (`input input-bordered flex items-center`) was hand-rolled 24
  times, so every blade defect was an N-place defect: `display_name` was labelled "Name" on 11 blades
  at once, and a description that would not wrap was one bug in 24 fields
  ([#573](https://github.com/hyperscaleav/omniglass/issues/573)). The seed-owned vendor blade made
  the treatment question concrete: five fields rendered as bordered boxes on a panel with no pencil,
  two of them holding a placeholder glyph, directly below three plain facts. The same read-only state
  had two appearances on one panel, and the boxed one signalled an editability that did not exist.
  A box that rejects typing reads as broken; a fact reads as a fact. The rule also fixes the read half
  of #573 by construction, because text in a fact wraps. Shipped by
  [#575](https://github.com/hyperscaleav/omniglass/issues/575) in the
  [#574](https://github.com/hyperscaleav/omniglass/issues/574) loop.
