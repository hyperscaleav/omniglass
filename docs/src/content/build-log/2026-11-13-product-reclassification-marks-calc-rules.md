---
title: "Product reclassification marks calc rules (#1030)"
date: 2026-11-13
refs: [1030, 1015]
---

A product's component type change (`UpdateProduct`) now marks the calc rules of every component of that
product, as a single component's product change already did (#1015). One statement marks every enabled
component rule that selects by type on each of the product's components that holds one of its inputs or
a state row of its own, so a product with thousands of components is one round trip. The serve rechecks
selection: a rule the product moved into computes over the values already stored, without waiting for a
new sample, and a rule it moved out of drops its state there. A patch that leaves the type (a label, a
vendor, or the current component type restated) marks nothing.
