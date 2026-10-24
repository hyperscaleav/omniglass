---
title: "Property, sample, and current value replace the datapoint"
date: 2026-07-28
status: "Accepted; superseded in part by [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)"
summary: "Property, sample, and current value replace the datapoint"
---

- **Date:** 2026-07-28 | **Status:** Accepted | **Pages:** [properties](/architecture/properties/), [storage](/architecture/storage/)

The one word "datapoint" conflated two things: the signal (what is measured) and the observation (a single reading of it). Splitting them removes the ambiguity that let "the datapoint's current value" and "a datapoint arrives" name different nouns.

- A **property** is the canonical signal on one owning entity (the key a sample observes and config declares); its registry is `property_type`.
- A **sample** is one timestamped observation of a property, a row in `metric` / `state` (the kind).
- The **current value** is the latest sample per series, held in the `property` cache.

No tables were renamed (`metric`, `state`, `event`, `property`, `property_type` stay); the shift is vocabulary across the code (the proto `Sample` message, the `*Sample` Go types, `deriveSamples`) and the docs (the value-model page is now [properties](/architecture/properties/), with a redirect from the old slug).

It also settles the built shape the earlier design left open: the **`property` cache is the architecture-of-record for current values**, a table upserted from the persistence sink with an out-of-order guard, not the "view over metric" once sketched on [storage](/architecture/storage/). The `intended` value a command opens is written in the command's Postgres transaction; the data-lane re-entry of intended and the adaptive-poll reconciliation described under [intended](/architecture/properties/#intended-the-declared-effect-of-a-command) are the deferred **actuation** evolution, not the built path. Refines [ADR-0063](#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables).
