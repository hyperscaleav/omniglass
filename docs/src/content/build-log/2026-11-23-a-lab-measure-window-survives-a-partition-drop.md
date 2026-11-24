---
title: "A lab measure window survives a retention partition drop (#1956)"
date: 2026-11-23
refs: [1956, 1623]
---

A lab measure sample spanning a retention partition drop failed with "the database's counters went
backwards (Postgres restarted?)" though Postgres never restarted: `DBCountersQuery` summed
`n_tup_ins` over the partitions present at read time, and dropping a partition removes its stats
row, so the sum fell. A long soak lost a sample at every daily roll.

The query now also returns the insert counter per partition, and `WindowOf` differences each
partition present at both ends: a dropped one contributes nothing, a new one counts from zero, and
a decrease within one partition still fails as a restart. Regression tests:
`TestWindowOfAcrossDroppedPartition` and `TestIntegrationWindowAcrossDroppedPartition` in
`internal/labfootprint`.
