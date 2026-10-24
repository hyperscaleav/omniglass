---
title: "\"Latest\" in a calculated series means the highest id"
date: 2026-07-22
refs: []
form: bullet
---

- **"Latest" in a calculated series means the highest id**
  ([ADR-0061](/architecture/decisions/#adr-0061-a-calculated-series-is-current-at-its-highest-id-not-its-newest-timestamp)):
  a health row's `ts` is `clock_timestamp()` evaluated before its identity id is assigned, so two
  concurrent inserts can land with the two orderings disagreeing about which row is current. Every
  production reader already ordered by id; the health test helper ordered by `ts`, which is what made
  `make test` intermittently red with a verdict the engine never produced. `LatestState`, which backs the
  ingest transition guard, ordered by `ts` alone and now tie-breaks on id, so a poll cycle stamping
  several rows in one instant can no longer resolve to an arbitrary one. Reproduced deliberately (six
  concurrent copies of the storage package; never once in nine idle full-suite runs) and verified over 24
  runs under the same load.
