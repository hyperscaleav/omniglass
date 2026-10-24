---
title: "The `property` latest-value cache and the reconciliation read land ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), first feature slice)"
date: 2026-07-27
refs: []
form: bullet
---

- **The `property` latest-value cache and the reconciliation read land ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), first feature slice).**
  `property` becomes the single-lookup latest-value cache for the producer provenances: the telemetry
  ingest sink, after appending the `metric`/`state` sample, derives the newest value per `(owner,
  property_type, instance, provenance)` series through a new `UpsertProperties` gateway, where an
  out-of-order guard keeps a late older sample from displacing a newer latest. The derive is
  **non-gating** (a failed upsert is logged, never fails the ack, and the cache is rebuildable from the
  append-only samples) and idempotent, so a redelivery does not double-write. `LatestValue` returns the
  current value in one lookup instead of an `order by ts` scan, and a **reconciliation** read (`GET
  /components/{name}/reconciliation`) pivots **want** (declared, resolved live from the cascade via
  `EffectiveProperties`, never a cache row), **told** (intended), and **is** (observed), with config-drift
  computed on read; a read-only Reconciliation panel on the component detail teaches the pivot. Only
  `observed` has a live producer today; `intended` (the command pillar) and `calculated` (the calc engine)
  are tested cache slots. One additive migration adds the value's own `ts` to `property`. The
  [datapoints](/architecture/properties/) and [config, secrets, and variables](/architecture/variables/)
  pages stay `Partial` (the cache and reconciliation are built; the intended/calculated producers,
  log-to-event, calc, and fusion remain). Part of
  [#394](https://github.com/hyperscaleav/omniglass/issues/394).
