---
title: "Retention is provenance-aware: never declared, never the latest row per series"
date: 2026-08-05
status: "Accepted"
summary: "retention is provenance-aware: a prune never deletes a declared row and never the latest row of any series, shipped as the PruneSamples primitive before any retention feature exists"
---

- **Date:** 2026-08-05 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [samples](/architecture/properties/)
- **Decision:** any retention pass over the sample tables obeys two invariants: it **never deletes
  a `declared` row** (an operator's assertion is the whole truth however old, not a sample that
  ages out), and it **never deletes the latest row of any series** `(type, owner arc, instance,
  provenance)` (a prune must not erase a current value). The rule ships as the **`PruneSamples`**
  Storage Gateway primitive, tested for both invariants, **before any retention feature exists**;
  no caller wires it yet, and any future retention feature calls the primitive rather than writing
  its own delete.
- **Context:** a blanket "delete older than N days" is the obvious first retention feature, and it
  would silently erase a declared value set two years ago, because for declared provenance the
  single row is the record itself, not one sample among many. With current values now derived from
  the series ([ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)), a
  naive prune would also delete the newest row of a quiet series and blank a current reading. Both
  traps are cheap to close before the feature and expensive to discover after a purge, so the floor
  shipped ahead of the feature (#591).
