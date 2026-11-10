---
title: "Every telemetry item is accounted on its batch's receipt and read as a loss ratio"
date: 2026-11-09
status: "Accepted"
summary: "Delivery is measured as one loss ratio per telemetry kind per hop, `(in - out) / in`, 0 healthy (#1641). A node's spool stamps each batch with a ledger (boot, dense seq, minted, resolved, per-kind count and running offset, field 16 of `TelemetryBatch`); the landing transaction writes the ledger and the committed pass's per-kind outcome on the batch's receipt. Refusals are loss, an unbound identity is accounted for (amended by ADR-1659), deduplicated items sit beside the ratio, a gap in a boot's seq belongs to its lower neighbor's window and reads as missing from the offsets, and a boot whose gaps could still fill reads unsettled with no ratio."
refs: [1641, 1623, 453, 1595, 1639, 1640, 1625, 1628, 1691, 1692, 1696, 1698, 1699]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio), [nodes](/architecture/nodes/#delivery-a-batch-lands-once), [scaling](/architecture/scaling/#ingest-one-batch-per-tick), [lab](/guides/lab/#reading-the-delivery-table)
- **Decision:** (#1641, the first slice under #1623.)
  1. **One measurement, one shape.** Every kind (metric, property, event, log) at every hop reads
     `loss = (in - out) / in`, the kind a value and never part of a key. Out is the items accounted
     for in the store: `stored`, and `coalesced` (an unchanged property the transition guard skipped,
     #1595). A kind with nothing in has no ratio, never a 0; a hop a kind does not cross has no row.
     End to end is `1 - prod(1 - loss)` over the hops a kind crosses.
  2. **The ledger rides on the batch.** The spool stamps each batch, under its lock, with a
     `DeliveryLedger`: a `boot` minted when the spool starts, a dense `seq` from 1, the node clock's
     `minted`, `resolved` (every batch at or below it had left the spool) and per kind the `count` it
     carries and the `before` the boot minted earlier. A missing batch is a hole in `seq` whose items
     its neighbors' offsets give, in any arrival order; `resolved` says when a hole can no longer fill.
  3. **The receipt is the accounting row.** The landing transaction writes the ledger and the outcome
     of the pass that commits on the batch's `telemetry_receipt` row, so the accounting is exactly-once
     with the data and adds no rows to the footprint being measured. A redelivery the receipt absorbs
     counts one more `deliveries` and lands nothing; the first delivery is told by `xmax = 0`, since
     an `on conflict do update` reports one affected row either way.
  4. **Refusal is loss.** Every refusal (`refused-unregistered`, `-writer`, `-instance`, `-invalid`,
     `-empty`, `-write`), `orphaned` and `unidentified` count as loss, and the read names the
     largest outcome behind a loss as its cause. Each item takes exactly one outcome. **Amended
     (#1659):** `unbound` counts as out, not loss
     ([ADR-1659](#adr-1659-a-hop-writes-its-delivery-in-out-and-loss-ratio-per-kind-every-window)).
  5. **Deduplicated sits beside the ratio.** In and out count distinct items; a redelivery's items
     read as `deduplicated` next to the ratio and never move it.
  6. **A gap belongs to its lower neighbor.** A window counts the receipts minted in it and the gaps
     above them; a gap below a boot's lowest receipt is that receipt's. Each gap has one owner, so
     adjacent windows sum to one. A boot with no batch minted after the window, or whose resolved has
     not reached past the window, reads `unsettled` with no ratio, as does every boot while the stream
     still holds batches.
- **Context:** The signals of loss existed (a log-scraped redelivery count, receipt inserts that
  include rolled-back passes, a dozen drop paths that warned or stayed silent), but each needed
  insider knowledge to read, and none said how many items a run lost. A uniform ratio with 0 healthy
  can be read, and gated, without that knowledge.
- **Thin cuts:**
  - The outcome is tallied per batch from the committed pass (`IngestBatch.outcome` over the live and
    refused groups and the consumer's own tally), not from per-group written and coalesced counts on
    `LandResult` as the issue proposed; the coalesced count comes from the property write directly.
  - The push lane records no ledger and reads no row (#1696); a whole-batch Term leaves no receipt
    and reads as `missing` (#1691); a node's spool drops and a killed boot's tail read as ingest
    `missing` or not at all until the node hop (#1692).
  - The read finds a boot's lower neighbor by seq, however long before the window it was minted, while
    its receipt is kept (a day): a neighbor pruned before a long outage ends reads the gap below the
    window as `missing`. The window is capped at 23 hours by the receipts' day.
  - The settle reads the stream's backlog from the server's own `bus-consumer-pending` and
    `bus-consumer-ack-pending` platform gauges (#1628), not from JetStream directly.
  - The induced unregistered-key end-to-end run is #1698, and the healthy end-to-end test ran by
    hand against a private image, not through `make test-lab`, which rebuilds the shared dev image.
  - The receipt grew from about 290 to about 675 bytes a batch, past the scaling target at 1,000
    systems; #1699 narrows it or revises the target.
- **Not chosen:**
  - **Counting refusals as out.** A fleet refusing a third of its samples would read 0.
  - **New accounting rows.** They perturb the footprint they measure (#1639); columns on the receipt
    commit with the data and cost no extra row.
  - **Keying on the batch id.** A UUIDv7 is time-ordered but not dense, so a missing batch leaves no
    hole in it.
  - **Counting deduplicated items on both sides.** A retry storm would dilute the ratio and could hide
    real loss.
