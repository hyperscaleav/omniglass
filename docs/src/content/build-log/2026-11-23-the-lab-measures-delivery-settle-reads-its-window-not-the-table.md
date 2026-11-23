---
title: "The lab measure's delivery settle reads its window, not the table (#1947)"
date: 2026-11-23
refs: [1947, 1841, 1896, 1623]
---

While ingest was behind, `lab measure` re-ran `ReceiptsQuery` every 5 s for the whole
`--settle-timeout`, and the query window-sorted every ledgered row of `telemetry_receipt` (about 3
million on the 3,000-system soak), spilling to disk with parallel workers. The settle loaded the
database it was measuring for most of every run.

Two changes. The query now picks the boots that minted in the window from a partial `minted` index,
then reads each boot's receipts at or above its first in-window seq, its lower neighbor and the
restart probe from a partial `(boot, seq)` index (migration `telemetry_receipt_window_indexes`), so
it costs what its window holds. And the settle polls the cheap backlog first and re-reads receipts
only when the consumer's ack floor has moved past what the last read saw, since a batch lands only
as it is acked. Tests: the bounded query returns the old query's rows on the fixture (the old query
kept as the oracle), its plan uses the new indexes and no sequential scan, and a settle whose floor
holds reads receipts once per floor advance.
