---
title: "Templates are redefined, not deferred ([ADR-0071](/architecture/decisions/#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins))"
date: 2026-07-31
refs: []
form: bullet
---

- **Templates are redefined, not deferred
  ([ADR-0071](/architecture/decisions/#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins)).**
  The decision log and the code had drifted into disagreement and neither said so. ADR-0045 deferred "a product's own
  template or field-schema binding" and ADR-0049 stated "Templates and their frozen BOM stay `Design`", so the log
  asserted the pinned model was merely *deferred*, while the shipped schema pointed a component at `product_id` and a
  system at `standard_id` and ADR-0047 and ADR-0048 had retired `component_type` and `system_type` outright.
  A template is now an **example configuration an operator clones**: forking is one-time, with no inheritance and no
  back-pointer, so **a template is upgrade-safe precisely because nothing stays connected to it**. The versioned-shape
  model retires with the pin (`component_template`, `system_template`, their `*_version` rows, the `stable` / `beta`
  channels, the frozen BOM, "an instance pins a version"). A component's shape is its **`product`**; a system's is the
  **`standard`** it **conforms** to with live inheritance. Forking applies template to row, conformance applies row to
  instance, and only the second is live. The word survives with its purpose inverted: the pin existed so a template
  could not change under an instance, the fork exists so it can change freely.
  `templates.md` is rewritten to the fork model (its frontmatter description carried the retired nouns into search
  results), the cascade loses two rungs (a template cannot be a resolution tier when nothing points at it), and the
  glossary's two shape rows become supersession entries. Two denylist entries land with it, scoped to the snake_case
  identifiers on purpose: the *word* template is not retired, so prose like "start from a template" stays correct.
  Still `Design`: the template loader, its catalog, and the create-from-template flow
  ([#317](https://github.com/hyperscaleav/omniglass/issues/317)), which now also covers instantiating a whole system.
