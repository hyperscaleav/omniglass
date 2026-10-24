---
title: "The datapoint concept retires for property, sample, and current value ([ADR-0065](/architecture/decisions/#adr-0065-property-sample-and-current-value-replace-the-datapoint))"
date: 2026-07-28
refs: [405]
form: bullet
---

- **The datapoint concept retires for property, sample, and current value ([ADR-0065](/architecture/decisions/#adr-0065-property-sample-and-current-value-replace-the-datapoint)).** The one word "datapoint" split the signal from the observation: a **property** is the signal (registry `property_type`), a **sample** is one timestamped observation of it (a `metric` / `state` row, the kind), and the **current value** is the latest sample per series (the `property` cache). A behavior-preserving rename, no tables touched: the proto `Datapoint` message becomes `Sample`, the metric and state datapoint Go types become `*Sample`, `deriveDatapoints` becomes `deriveSamples`, and the value-model page is now [properties](/architecture/properties/) (redirect from the old slug), which carries the current-value cache and want/told/is reconciliation explainer. The ADR also settles that the built `property` cache, a table upserted from the sink rather than the once-sketched metric view, is the architecture-of-record for current values. Part of [#405](https://github.com/hyperscaleav/omniglass/issues/405).
