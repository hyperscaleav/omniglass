---
title: "Field cascade and the type-default floor"
date: 2026-07-19
status: "Accepted"
summary: "A field's resolved value is deepest-set-wins down `product -> location -> system -> component`, falling to the field's **type default** when nothing is set at any scope; the type default is the **floor** of the cascade, not a competitor to it. This slice is component-only (resolved = the component's set value, else the type default); the multi-scope cascade is tracked by #291"
---

- **Date:** 2026-07-19 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** A field's **resolved value** is **deepest-set-wins** down the field arc
  `product -> location -> system -> component`: a value set at any scope beats every broader scope. When
  **nothing is set at any scope**, the value falls to the field's **type default**. The type default is
  therefore the **floor** of the cascade, not a competitor to it: a value set at any higher scope always
  beats the default, and no cascade rule is bent to make that true. Raised during design as "does a value
  set higher in the cascade beat the type default?", the answer is **yes**, and it costs the model nothing,
  because the default is simply the bottom rung.
- **Context:** The override-rendering slice needed the resolution rule pinned before the renderer could say
  what "inherited" means. Modelling the type default as a **competing scope** would force an ordering
  question at every read (does a `location` value or the type default win?); modelling it as the **floor**
  removes the question: any set value at any scope wins, and the default is what remains when the arc is
  empty. This slice is **component-only**: resolved = this component's set value, else the type default, so
  the deeper arc (`product`, `location`, `system`) is drawn in the model but not yet walked. The
  multi-scope cascade itself lands later.
- **Closes the gap:** the multi-scope cascade is tracked by
  [#291](https://github.com/hyperscaleav/omniglass/issues/291); this ADR settles only the resolution rule
  and the type-default floor.
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):**
  the resolution outcome is unchanged (any value set at any scope beats the default, and the default is what
  remains when the arc is empty), but the default is **off the axis**, a column on the definition row, rather
  than the cascade's bottom rung. The vocabulary moved; the rule did not.
