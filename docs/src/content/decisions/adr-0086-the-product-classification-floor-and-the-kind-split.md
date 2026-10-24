---
title: "The product classification floor, and the kind split"
date: 2026-08-07
status: "Accepted"
summary: "Every component is required to name a product (the three seeded generics cover anything unmodeled); product.kind narrows to device / app / service, no default, required at create; vm retires, folded into app"
---

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [Products guide](/guides/admin/products/)
- **Decision:** Every **component** is required to name a **product**: `component.product_id` is
  `NOT NULL`. Three seeded generics (`generic-device`, `generic-app`, `generic-service`, each pointed
  at the matching generic `component_type`) cover anything not yet modeled as a real SKU, so the floor
  is total from the first migrated database onward: no component ever exists with no kind, no declared
  contract, and no driver path. **`product.kind`** narrows to `device | app | service`, drops its
  column default, and is required at create: an operator states a product's class explicitly rather
  than reading a silent fallback to `device` that let a mislabeled cloud service pass as correct
  forever. **`vm` retires**, folded into `app`, because nothing forks on a virtual machine that does
  not fork the same way on any other app: a virtual appliance is a different SKU, not a different kind.
  The three that remain are the who-owns-what split: `device` (the box is yours), `app` (the runtime is
  yours), `service` (only the account is yours).
- **Context:** ADR-0049's own Context named the optional product as the forcing function behind
  capability-gated staffing's resolved-capability layering: a role could not check a product's
  capabilities directly because a component might not have one. Making product required closes that
  gap at the root rather than compensating for it one layer up, and does the same for the naming
  epic ([ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus)): a
  generated component name needs a `component_type`, which needs a product, so "product required" is
  what makes the naming generator total rather than a fallback-to-hand-authoring special case. The
  `vm` retirement follows the same audit that found the kind default silently absorbing a mistake: a
  fixed four-value enum with a default reads as validated when it is merely unset, and the fourth value
  had no code path that branched on it the other three did not already cover.
- **Tracked under** epic [#614](https://github.com/hyperscaleav/omniglass/issues/614).
