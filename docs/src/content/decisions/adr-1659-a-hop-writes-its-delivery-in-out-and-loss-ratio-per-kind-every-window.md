---
title: "A hop writes its delivery in, out and loss ratio per kind every window"
date: 2026-11-10
status: "Accepted"
summary: "Accepted: the node and the server each write their own hop's delivery as three platform keys, `delivery-in-count`, `delivery-out-count` and `delivery-loss-ratio`, the item kind as the instance, every report window, 0 when quiet, the ratio written as 0 when nothing came in. They replace `spool-drop-count`, `publish-failure-count` and `bus-telemetry-drop-count`. An item counts when it settles; a deduplicated item and a held redelivery count in and out, so a replay reads 0; a Termed batch's items count in and not out, an undecodable batch, naming no items, counts nothing on the server hop; and an unbound identity is accounted for, not loss, at every hop and in the ledger read (amending ADR-1641)."
refs: [1659, 1641, 1623, 1625, 1627, 1628, 1644, 1646, 1692, 1712]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#self-monitoring-the-node-reports-itself), [collection](/architecture/collection/#self-monitoring-every-nodes-account-of-itself), [messaging](/architecture/messaging/#measuring-the-pipeline)
- **Decision:** (#1659, under #1623.)
  1. **Three keys, every hop, the kind as the instance.** `delivery-in-count`, `delivery-out-count`
     and `delivery-loss-ratio` (`(in - out) / in`, 0 healthy) are declared once and written by both
     the node (on its node-self block) and the server (through its window recorder), each on its
     own platform component. They fold `spool-drop-count` and `publish-failure-count` (#1625) and
     `bus-telemetry-drop-count` (#1628, amending
     [ADR-1628](/architecture/decisions/#adr-1628-ingest-lag-is-the-commit-time-minus-the-sample-ts-read-on-the-server)),
     which are gone from the seed, the writer class and the platform view.
  2. **Every window, 0 when quiet.** All three land every window for every kind, the counts 0 and
     the ratio 0 when nothing came in. The issue asked that a quiet window emit nothing; the
     architect's 2026-11-10 ruling replaces that: a series that stops writing leaves its last loss
     standing as the latest reading, so a hop that dropped once and then went quiet would read lossy
     until its next item. This diverges from the ledger read
     ([ADR-1641](/architecture/decisions/#adr-1641-every-telemetry-item-is-accounted-on-its-batchs-receipt-and-read-as-a-loss-ratio)),
     where a kind with nothing in reads no ratio: the ledger read is over a chosen span, a platform
     series is a latest value.
  3. **An item counts when it settles, at the hop that settles it.** On the node: a batch the
     stream acks counts every item in and out; one the spool drops at a cap or by age, one the bus
     refuses, and a group that never encodes count in and not out; a batch held for a retry has not
     settled and counts in the window it does, once. On the server: a landed batch counts the
     store's outcome (stored, coalesced and unbound out, refused and orphaned not), and a Termed batch
     (refused whole, its fifth delivery, unreadable metadata, an owner or task it may not assert)
     counts every item it names in and none out.
  4. **A replay reads 0.** A deduplicated item counts in and out (an earlier copy landed it), and a
     redelivery its receipt holds to a no-op counts its items in and out, so neither a lost ack nor
     a replay moves the ratio. Unlike the ledger read, where deduplicated sits beside the ratio, a
     window here has no receipt to name distinct items by, so the hop counts what it handled.
  5. **An undecodable batch counts nothing on the server hop.** It names no items, so it cannot add
     to in; its items read as `missing` on the ledger, and naming them on the hop is #1712.
  6. **The API lane tallies its rejects.** A push's samples refused at derivation now enter the
     batch's outcome, so they count as lost on the server hop as a node batch's do.
  7. **An unbound identity is accounted for, not loss.** The architect's 2026-11-10 ruling: an
     identity a conduit reports that no binding places (the outcome `unbound`, such as the dev
     seed's deliberately unassigned UCC room `zr-boardroom-b`, kept per #1390) is configuration
     state, not data omniglass dropped. The operator has not assigned it, and the conduit's unbound
     inbox already surfaces it (its count, keys and heard count); like a device that fails to
     answer, it is accounted for. It counts in and out on the server hop and in the ledger read, so
     a healthy fleet with an unassigned identity reads 0, and its count still prints on every row.
     This amends
     [ADR-1641](/architecture/decisions/#adr-1641-every-telemetry-item-is-accounted-on-its-batchs-receipt-and-read-as-a-loss-ratio)
     decision 4, which counted it as loss; refused, orphaned, unidentified and missing stay loss.
- **Context:** #1641 defined one loss ratio per kind per hop and read the ingest hop from receipts.
  The node and server still reported their drops as bespoke counts, in batches rather than items,
  each needing knowledge of its reasons to read. Folding them into the uniform keys lets #1646's
  Loss group, and any rule, read every hop the same way.
