---
title: "The property lane maintains one series index, not two (#971)"
date: 2026-10-21
refs: [971]
---

`property_owner_idx` (the #588 rename of `state_owner_idx`) and `property_series_idx` (#944) were the same index: btree on `(component_id, property_type_id, instance, ts desc)`, partial on `component_id is not null`, so every property insert maintained it twice. `20261022010000_drop_property_owner_idx.sql` drops the older name and keeps `property_series_idx`, the one `TestSeriesNewestIsAnIndexProbe` explains its reads against. `TestNoDuplicateIndexes` groups `pg_index` by table, access method, key columns, operator classes, collations, sort options, expressions, predicate and uniqueness and fails on any group of two, so a twin cannot land on any table again; this pair was the only one in the schema.
