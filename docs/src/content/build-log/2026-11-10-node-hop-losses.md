---
title: "The delivery table reads the node hop (#1692)"
date: 2026-11-10
refs: [1692, 1641, 1659, 1691, 1733]
---

`omniglass lab measure` now prints a `node` row per kind above the `ingest` row: what the node minted
against what its spool shipped, with the node's own causes (`spool-overflow`, `spool-expired`,
`bus-refused`, `self-log-overflow`, `encode-failed`). The spool notes each batch it drops as a run on
its `delivery.Ledger`, and every batch minted after carries the run until one is acked; the self-log
buffer and the encoder add running totals of items lost before minting. The `DeliveryLedger` gains
`dropped` and `lost`, and the landing transaction keeps them on the receipt's new `node_loss` column.
`delivery.ReconcileNode` reads the node hop from the same receipts as the ingest read, so a hole the
node accounts for is its loss and no longer ingest `missing`, the node's out equals the ingest's in,
and end to end composes the two. A spool overflowed by a bus outage, read against real Postgres and
JetStream, reads as node loss with the ingest row at 0. A boot killed with batches in its spool is
named as `unshipped` beside the table rather than left unsettled; counting its tail is #1733. The
decisions are
[ADR-1692](/architecture/decisions/#adr-1692-the-node-hop-reads-its-losses-from-runs-the-next-batches-carry).
