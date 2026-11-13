---
title: "Product reclassification marks calc rules (#1030)"
date: 2026-11-13
refs: [1030, 1015]
---

A product's component type change (via `UpdateProduct`) now brings the calc rules of every component of
that product in line with the selection, the same as a single component's reclassification does (#1015).
When a product is retyped out of a rule's component type selector, that rule's marks and state rows are
dropped on every component of the product. When a product is retyped into a rule's selector, the rule is
marked on every component holding one of its input keys, so the rule computes on the next tick without
waiting for a new sample.

The fix inserts one set of calc_pending marks and deletes marks and state in a single statement each,
so a product with thousands of components is one round trip. A product patch that restates the current
component type inserts no marks and is not checked, following ADR-0030's forward-only contract.
