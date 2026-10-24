---
title: "A product with no label stops wedging its components"
date: 2026-08-26
refs: []
form: bullet
---

- **A product with no label stops wedging its components**
  ([#805](https://github.com/hyperscaleav/omniglass/issues/805)). `componentLabelChain`
  scanned `p.label` into a plain string while every other label read coalesced, so a product
  whose label is NULL failed every component create, rename, move and reclassify that
  resolved it. The API cannot mint that row (create requires `minLength: 1`), but the
  direct-gateway lane can, and that lane is the one seed, devseed and the tests use. The
  query coalesces like its siblings; the label falls through to the component_type, which is
  what an absent product label should render as. Found by the #804 sweep, whose harness had
  worked around it by setting labels.
