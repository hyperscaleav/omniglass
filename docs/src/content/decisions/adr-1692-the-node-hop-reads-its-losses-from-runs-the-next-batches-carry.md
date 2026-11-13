---
title: "The node hop reads its losses from runs the next batches carry"
date: 2026-11-10
status: "Accepted"
summary: "Accepted: a node's own losses ride the `DeliveryLedger` of the batches it mints after them, as runs of dropped seqs with their cause and items (field 6, carried until a batch is acked) and running totals of items lost before any batch carried them (field 7). The receipt keeps them in a `node_loss` column. The read owns a dropped run with the gap it sits in, so a hole the node accounts for is its loss under its cause and the rest is ingest `missing`, and the node's out is exactly the ingest's in. A killed boot's unshipped tail is named beside the table, not counted, until #1733."
refs: [1692, 1641, 1659, 1691, 1733, 1623]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio), [nodes](/architecture/nodes/#delivery-a-batch-lands-once), [lab](/guides/lab/#reading-the-delivery-table)
- **Decision:** (#1692, under #1623.)
  1. **A drop is reported by the batches after it.** A batch the spool gives up on (the count or byte
     cap, its age, a bus refusal) took a seq and never ships, so only a later batch of the boot can
     say what it was. The ledger carries `dropped`, runs of consecutive seqs dropped for one cause
     with their items per kind, on every batch minted after the drop until one of those batches is
     acked; the read keeps the longest copy of each run. Carrying runs rather than per-batch entries
     keeps a long outage to one entry; past 256 runs the oldest is forgotten and its items read as
     ingest `missing`, never vanish.
  2. **Items lost before minting are running totals.** Self-log lines the buffer dropped and groups
     that never encoded have no seq. The ledger carries `lost`, their totals per cause and kind since
     the boot began; a window's share is the most any in-window receipt carries less the most any
     receipt below the window carries, so adjacent windows still sum.
  3. **The receipt keeps the node's account beside the ledger.** A `node_loss` jsonb column on
     `telemetry_receipt`, written in the landing transaction like the outcome, rather than new rows:
     the account commits exactly when the batch does.
  4. **The node's out is the ingest's in.** A gap is owned as ADR-1641 owns it; of its items, those
     the runs inside it account for are the node's loss under their cause (capped at the gap's items),
     the rest the node shipped and the ingest hop reads as `missing`. End to end composes the two
     with `1 - prod(1 - loss)`.
  5. **A killed boot's tail is named, not counted.** The spec asked the read to report it as
     `unshipped`. A boot followed by a later boot of the same node can no longer settle, so the read
     takes it as far as it landed and prints its last landed seq and when the next boot began. How
     many items the tail held is in no landed batch: counting it needs the next boot to report the
     old one's watermark, which is #1733.
- **Context:** #1641 read the ingest hop from receipts, so a spool drop read as ingest `missing` and
  a killed boot's tail not at all. Naming the node's causes lets the ingest row read only what was
  lost after the node shipped it, and gives the platform's node hop (#1659) causes to point at.
