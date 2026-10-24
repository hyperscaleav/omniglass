---
title: "The blade field becomes a primitive"
date: 2026-08-04
refs: [574]
form: bullet
---

- **The blade field becomes a primitive** ([#574](https://github.com/hyperscaleav/omniglass/issues/574)).
  The blade *shell* was a primitive and the blade *contents* were not, so every blade defect was an
  N-place defect. The identity work paid that three times in one body of work: `display_name` was
  labelled "Name" on 11 blades, a long description failed to wrap in 24 fields, and 3 blade headings
  rendered a raw id. Counting the duplication before designing anything found eleven pages defining a
  byte-identical local `Field` (all eleven bodies hashing the same), a twelfth idiom in three
  create-as-route forms, positional `ctx.field(...)` / `ctx.fact(...)` helpers on four more, and the
  read-only box hand-rolled 24 times.

  Reading the code changed the answer. The label-and-control wrapper already existed as `FieldRow`,
  and it was better than the eleven copies: it associates the label to the control by id with the
  tooltip trigger outside the `<label>`, where the copies wrapped the control in a `<label>` and
  broke its accessible name. Both positional helpers already delegated, to `FieldRow` and
  `KVStacked`. So the duplication was not two implementations, it was one implementation behind five
  call forms plus eleven pages that had adopted none of them. `BladeField` is therefore not a new
  wrapper but the read-or-edit switch composed over the two that existed, and no `BladeFact` was
  added, because `KVStacked` already was one.

  **A read-only field renders as a fact, not a box that refuses typing** ([ADR-0078](/architecture/decisions/)).
  The question was invisible in code and obvious in a screenshot: the seed-owned vendor blade, which
  nobody can edit, rendered five bordered input boxes directly below three plain facts. One
  read-only state, two appearances, and the boxed one signalled an editability that did not exist.
  Making the read state a fact also fixed half of [#573](https://github.com/hyperscaleav/omniglass/issues/573)
  by construction, because text in a fact wraps.

  The identity pairing stopped being a convention and became a type. A field or fact that shows one
  of the two words names the fact it is bound to (`bind="display_name"`) and takes its label from
  `IDENTITY_LABELS`; `label` is refused alongside `bind` on all three components. The guard that had
  enforced this by scanning source shrank accordingly: gone are the four-alternate regex, the
  eight-line lookahead window, its terminator, and the `display()` heuristic, all of which existed
  only because 74 sites paired a label to a binding by hand. What is left is the one failure a type
  cannot catch, a page bypassing the components, and it was mutation-tested three ways before being
  believed, including a multi-line tag the old line-based regex would have missed.
