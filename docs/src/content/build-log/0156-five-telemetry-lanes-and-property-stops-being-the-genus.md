---
title: "Five telemetry lanes, and property stops being the genus"
date: 2026-08-05
refs: []
form: bullet
---

- **Five telemetry lanes, and property stops being the genus**
  ([#584](https://github.com/hyperscaleav/omniglass/issues/584),
  [ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus),
  [ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)).
  Seven slices on one integration branch, and at the end of them an operator meets five lanes with
  five names and no overlap: a **metric** is a quantity, a **property** is a value, an **event** is
  a typed happening, a **command** is an instruction with a target, and a **log line** is raw
  arrival. Nothing here was greenfield; all five lanes had tables going in, so the epic was a
  catalog split, a fold, a rename, and a narrowing, the shape that hides defects in a large
  mechanical diff.

  In slice order: **one name rule, no dots** (#586) collapsed the two name rules into one single
  kebab token capped at 100 characters, renaming fifteen dotted keys across 83 files with a
  backfill that refuses on collision rather than picking a winner. **The catalog split** (#587)
  partitioned `property_type` on `data_type`, the lane key: numeric rows became `metric_type` with
  the numeric facts (unit, precision), rows kept their ids so every FK repointed as a pure
  constraint swap, and the `kind` column retired along with the `log` value its enum still
  advertised. **The fold** (#591) retired the value store: a declared value is a series row,
  an edit appends, an unset appends a JSON-null tombstone, and every current value is the latest
  series row, derived on read; `PruneSamples` shipped in the same slice as the caller-less
  retention floor (never declared, never the latest row per series, ADR-0080). **The rename**
  (#588) gave the text sample table the bare noun `property` once the store had vacated it, and
  converged the two value columns into one `value jsonb NOT NULL`; the word "state" left the schema
  (a health state is a concept, not a table). **The log split** (#589) narrowed `log_line` to
  component-only with node self-logs moving to `node_log`. **The command lifecycle** (#590) added
  the recorded status (`issued` / `settled` / `failed` / `timed-out`) beside the still-computed
  verdict, the two-armed target across both catalogs, and moved intended lineage onto the command
  itself (`command_id`). **The per-lane wire** (#594) closed the loop: `TelemetryBatch` carries
  `metrics` / `properties` / `events` / `logs` arrays, each validated against its own catalog, the
  polymorphic samples array reserved and gone, with the stored property encoding byte-identical
  across the break so transition detection stayed honest.

  Two mid-loop stops earned their keep. The **wrong-schema stop**: the epic definition had been
  drafted against the pre-July init-dump names (`property` the catalog, `property_value` the
  store), and verifying it against the running database before building found three claims pointing
  at tables that no longer existed under those names; the corrected map carried two rulings, the
  `data_type` partition key and derived-not-stored current values, and the same init-dump trap
  later caught two adversarial reviewers whose reds were refuted by reading the migrated schema
  instead of the dump. The **node_log ruling**: slice D as written would have orphaned the shipped
  self-log lane; the builder hit the stop condition, wrote nothing, and reported with evidence, and
  the architect amended the definition mid-loop to split the store by origin, after which the
  rebuild came back RED-first against the unmodified schema. The docs half landed the model on the
  architecture pages, appended ADR-0079 and ADR-0080, and put the retired vocabulary (`state` as a
  sample table, `property_value`, `StateSampleWrite` and siblings) on the docslint denylist.
