---
title: "Five telemetry lanes, and property stops being the genus"
date: 2026-08-05
status: "Accepted; the collective noun revised to signal lanes by [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes)"
summary: "five telemetry lanes with five names: the catalog splits on data_type, state renames to property, the value store folds into the series (tombstone unset, current values derived), the wire goes per-lane, logs split by origin, and a command records its status; reverses the property-as-genus half of ADR-0063/0065"
---

- **Date:** 2026-08-05 | **Status:** Accepted | **Pages:** [samples](/architecture/properties/),
  [storage](/architecture/storage/), [core entities](/architecture/core-entities/),
  [collection](/architecture/collection/), [commands](/architecture/commands/),
  [events](/architecture/events/), [data model](/architecture/data-model/),
  [health](/architecture/health/), [glossary](/architecture/glossary/)
- **Decision:** the telemetry model is **five lanes** with five names and no overlap: a **metric**
  is a quantity (numeric, aggregates, carries unit and precision), a **property** is a value (what
  something is, including a number used as a name; values have duration, not averages), an
  **event** is a typed happening, a **command** is an instruction with a target, and a **log line**
  is raw arrival. "Property" stops being the genus of every reading and becomes one of the five,
  and the word "state" retires as a table name: a health state is a concept, not a table. Shipped
  as epic [#584](https://github.com/hyperscaleav/omniglass/issues/584) in seven slices:
  - **The catalog splits on `data_type`, the lane key** (#587): `int` / `float` rows become
    `metric_type` (taking the numeric facts: unit, precision), `string` / `bool` / `json` rows stay
    `property_type` (keeping the value domain: validation), and the per-key `kind` column retires,
    since catalog membership routes a sample and partitions every row cleanly where `kind` did not
    (four kind-less declared names were properties by ruling, and `data_type='string'` already put
    them there). Each classifier contract gains a metric sibling (`product_metric`,
    `standard_metric`, `location_type_metric`). The three ingest catalogs share one resolution
    namespace: a create is refused when a sibling holds the name.
  - **The value store folds into the series** (#591): a declared value is an ordinary series row
    (`provenance='declared'`, no lineage), an edit appends, and the **unset is a tombstone** (an
    appended declared row whose value is JSON null, resolved as absence by every reader). The
    **current value of every provenance is derived**: the latest series row per
    `(type, owner arc, instance, provenance)`, never a maintained cache. The separate `property`
    value store and its upsert-on-intake cache retire outright.
  - **The `state` table becomes `property`** (#588): the record takes the bare noun and the catalog
    keeps the `_type` suffix, matching `event_type`/`event`; the two value columns converge into
    one `value jsonb NOT NULL`. The word "state" was overloaded (a health verdict is a state, a
    reachability verdict is a state, and neither was that table); the lane is a value over time,
    and its name now says so.
  - **A log line belongs to a component, and node self-logs split into `node_log`** (#589):
    `log_line`'s four-armed arc narrows to component-only, and a node's self-logs move to an
    origin-true `node_log` table keyed to the node, the amended ruling recorded on the slice issue.
  - **A command records its status, and an intended value names its command** (#590): `command`
    gains a recorded `status` (`issued` until a terminal `settled` / `failed` / `timed-out`, with
    `settled_at` stamping the terminal moment) beside the still-computed settlement verdict, and
    `command_type` gains the **two-armed target** (`target_property_type_id` or
    `target_metric_type_id`, never both), because a metric is commandable. An intended sample's
    lineage moves from the command's caused event to the **command itself** (`command_id`): the
    value points at its cause, not at a derivation of it.
  - **The push wire goes per-lane** (#594): `TelemetryBatch` carries `metrics`, `properties`,
    `events`, and `logs` arrays, each entry validated against its own catalog at ingest; the
    polymorphic samples array retires.
  - **One name rule, no dots** (#586): a name is a single kebab token, at most 100 characters, on
    every table; the dot-joined keyspace rule and its 128-character ceiling retire, and the seeded
    dotted names backfill to their hyphenated forms (`icmp.rtt-avg` to `icmp-rtt-avg`).
- **What this reverses, and why.** ADR-0063 and ADR-0065 recorded the previous taxonomy as the
  settled model: **property** as the canonical signal over every sample, one `property_type`
  catalog with a `kind` column spanning numeric and categorical names, and a maintained `property`
  latest-value cache as the architecture-of-record for current values. This ADR reverses that
  recorded call in part. The root defect was the **property-as-genus overreach**: one word carried
  the signal, the store, and the whole sample family, so "property" meant three things on one page,
  the catalog carried facts (unit, precision) that only half its rows could use, and the cache
  duplicated a fact the series already held. The registry-over-bare-noun pattern of ADR-0063
  survives intact; what changes is that property becomes one lane among five rather than the genus
  of all of them. Recorded as the current-best model on the evidence above, and like every entry in
  this log it is revisable if the evidence changes.
- **Context:** verified against the live schema before building, which produced one mid-loop stop:
  the epic was first drafted against the pre-July init-dump names, where `property` was the catalog
  and `property_value` held current values, and three of its claims pointed at tables that no
  longer existed under those names. The definition was corrected against the running database
  before any slice built on it, and two rulings rode the correction: the partition key is
  `data_type` rather than `kind`, and a current value is derived from its series rather than
  stored. The retired vocabulary (`state` as a sample table, `property_value`,
  `StateSampleWrite` and its siblings) joins the docslint denylist with this entry.
