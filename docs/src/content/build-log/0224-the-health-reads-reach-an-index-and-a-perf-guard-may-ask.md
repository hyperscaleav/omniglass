---
title: "The health reads reach an index, and a perf guard may ask the planner what a query can reach"
date: 2026-08-13
refs: []
form: bullet
---

- **The health reads reach an index, and a perf guard may ask the planner what a query can reach**
  ([#725](https://github.com/hyperscaleav/omniglass/issues/725),
  [ADR-0094](/architecture/decisions/) as amended). The bulk verdict read shipped in
  [#653](https://github.com/hyperscaleav/omniglass/issues/653) made a systems list cost one statement
  instead of one per row, and the win was entirely in round trips: `property` carried no index the read
  could use, so one scan of the whole series replaced N health resolutions. This slice is the other
  half, the work done inside that statement, and it starts by settling what a guard on it is allowed to
  ask.

  The issue framed it as a standoff between a house rule (a guard asks the planner, not the catalog)
  and an accepted ADR (no `EXPLAIN` assertions), and there is no standoff: ADR-0094 **deferred** them
  and named its own revisit conditions, neither of which is met here. What it deferred was asking the
  planner what it **prefers**, and it was right to. On a small fixture Postgres correctly chooses a
  sequential scan even over a perfect index, so an assertion about the chosen plan pins the fixture's
  size. Asking whether an index is **reachable at all** is a different question, and the difference was
  measured rather than argued: planned with `enable_seqscan = off`, the health reads' scan of
  `property` was the same index scan carrying the same index condition on an empty database, on a
  45-row fixture with no statistics, and on that fixture analyzed, while the joins around it moved
  between hash and merge and a sort came and went. So the deferral of preference assertions stands
  untouched and a **usability** assertion is carved out, as `internal/storage/storagetest/accesspath`:
  one relation's access path, index name **and** index condition, gating in `make test` beside
  round-trip counting.

  The condition half is not decoration. Two of the four mutations run against this guard leave the
  index NAMED in the plan and empty out its condition (coercing the filtered column, dropping the
  leading column from the filter), so an assertion that greps for the index name passes while the read
  walks the whole index and filters afterwards. A third, giving the partial index a predicate the query
  cannot prove, leaves `pg_indexes` reporting the index present and the read scanning the table, which
  is the failure this instrument exists for and the reason a catalog test would have proved nothing.
  The guard explains the statement the gateway really issued, captured off the pool tracer
  (`querycount.Counter.Calls` now records arguments beside SQL) rather than copied into the test.

  Then the index, and it earned its place on measurement, at 1,521,600 property rows (334 MB, 1,500
  systems, 7,500 components, 18,000 recorded health rows): the bulk read went from **51 ms to 10 ms**,
  the location rollup from **45 ms to 0.7 ms**, and the transition probe every recompute pays from
  **6.1 ms to 0.06 ms**. `property_system_owner_idx` leads with `property_type_id` because both reads
  fix it by equality, which leaves the rest of the range ordered by `(system_id, id desc)`, exactly
  their `ORDER BY`, so no sort is needed; the reverse order was built and measured slower once systems
  carry properties other than health. It is partial on `system_id is not null`, which is what makes it
  free on the telemetry lane: 150,000 component-owned inserts cost 2237 ms with it and 2237 ms without.
  A covering `include (value)` variant measured faster still (8.4 ms) and was rejected rather than
  shipped, because an index tuple is not TOASTed, so a property value over about 2.7 KB would fail the
  INSERT rather than the read. A concurrent build was rejected too, and the reasoning is in the
  migration: it cannot run in dbmate's transaction, and a failed one leaves an invalid index that the
  retry's `if not exists` silently skips, which is the same silently-unusable-index class this slice
  exists to close.
