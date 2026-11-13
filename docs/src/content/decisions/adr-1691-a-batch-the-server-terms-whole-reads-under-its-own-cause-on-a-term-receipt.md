---
title: "A batch the server Terms whole reads under its own cause on a Term receipt"
date: 2026-11-10
status: Accepted
summary: "Accepted: a batch the consumer Terms whole records a Term receipt, its ledger with every declared item under the cause (refused-owner, refused-task, refused-batch, refused-metadata, abandoned), each loss. The receipt is marked termed: a later copy of the batch that lands replaces it, a Term never overwrites a receipt, and the closed loop's receipt count skips it. An undecodable batch records nothing and stays missing, since its ledger cannot be read."
refs: [1691, 1641, 1639, 1659]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/), [storage](/architecture/storage/), [lab](/guides/lab/)
- **Decision:**
  1. **One outcome per Term path, each loss.** An owner asserted on the node lane reads
     `refused-owner`, task rows outside the groups `refused-task`, a batch the gateway refused whole
     `refused-batch`, unreadable delivery metadata `refused-metadata`, and the last delivery the
     consumer allows `abandoned`. Every item the batch's ledger declared takes the cause
     (`delivery.Declared.Under`), so the receipt reconciles with no shortfall.
  2. **A Term receipt is not a landing.** `telemetry_receipt.termed` marks it. A landing that finds
     it replaces it (its ledger, its landing time, one delivery, the landed outcome), so a node's
     retry past the stream's duplicate window still lands its rows. A Term that finds any receipt
     writes nothing: it never overwrites a landing, and the first Term's cause stands. The closed
     loop's receipt count (#1639) skips it, since it landed nothing.
  3. **Best effort.** A Term receipt that fails to record is warned and the batch is Termed anyway:
     holding it would stall every batch behind it, and its items then read `missing`.
- **Divergence:** the issue listed `refused-undecodable` among the outcomes. An undecodable batch has
  no ledger the server can read, so there is nothing to name items against: it records no receipt,
  its items stay `missing` (read from its neighbors' offsets), and the Term is warned. No
  `refused-undecodable` outcome exists.
- **Context:** before this a whole-batch Term left no receipt, so its items read `missing` beside the
  batches dropped at the node or lost in between (ADR-1641), and the footer could not tell them apart.
- **Rejected:** counting deliveries on a Term receipt (the batch's items read once, and its
  redeliveries are not copies a landing absorbed); letting a later Term replace an earlier one (the
  first cause is the one that stopped the batch); a separate table for Terms (the read already
  reconciles receipts, and one key keeps a batch's accounting on one row).
