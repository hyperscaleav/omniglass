---
title: "The observation tables lose the `_datapoint` genus"
date: 2026-07-23
refs: []
form: bullet
---

- **The observation tables lose the `_datapoint` genus**: `metric_datapoint` becomes `metric` and
  `state_datapoint` becomes `state`, matching their bare-noun sibling `event` on the principle that the
  noun is the entity and classification takes a `_type` / `_kind` suffix (the `datapoint_type` classifier
  already became `property`). Each table name now equals the `property.kind` value it stores
  (`kind='metric'` to `metric`), and the freed name `datapoint` stays available for the future UNION view
  over the kind-tables. Verified collision-free (not Postgres keywords, no existing table or column named
  `metric`/`state`/`log`). Dependent object names (pkey, sequence, indexes, FK and CHECK constraints) keep
  their `_datapoint` identifiers for now, as the `#262` renames left `vendor` and `standard`; the pending
  migration collapse cleans every stale identifier in one pass. Table-only rename, no data or wire change.
