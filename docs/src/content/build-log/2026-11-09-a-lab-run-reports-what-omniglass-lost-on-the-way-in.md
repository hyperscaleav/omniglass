---
title: "A lab run reports what omniglass lost on the way in, as one loss ratio per kind (#1641)"
date: 2026-11-09
refs: [1641, 1623, 1639, 1640, 1625, 1628, 1691, 1692, 1693, 1694, 1695, 1696, 1697, 1698, 1699]
---

`omniglass lab measure` now ends with a delivery table: each telemetry kind's loss across the ingest
hop, `(in - out) / in`, read the same way for every kind, 0 healthy, with the items a redelivery
carried again beside it and the largest outcome behind any loss as its cause
([ADR-1641](/architecture/decisions/#adr-1641-every-telemetry-item-is-accounted-on-its-batchs-receipt-and-read-as-a-loss-ratio)).
A node's spool stamps every batch with a delivery ledger (field 16 of `TelemetryBatch`: boot, a dense
seq, the minted time, how far the spool has resolved, and each kind's count and running offset). The
consumer gives each item exactly one outcome (stored, coalesced, one of six refusals, orphaned,
unbound, unidentified) and the landing transaction writes the ledger and the committed pass's outcome
on the batch's receipt, so the accounting is exactly-once with the data and adds no rows. A batch that
never landed is a hole in the boot's seq, read from its neighbors' offsets as `missing`; a gap belongs
to its lower neighbor's window, and a boot whose batches could still arrive reads `unsettled` with no
ratio. The receipt insert became an upsert that counts deliveries and tests `xmax = 0` for the first,
since `on conflict do update` reports one row either way.

Proven over a real stream and database (`internal/bus/delivery_test.go`): a withheld batch and a
max-delivered one read their items as missing, a held batch that lands after its successors reads
whole once the boot settles, and a redelivery reads as deduplicated with loss 0. Live, a
three-system corporate lab (`TestIntegrationScaleDelivery`, run against a private image) printed:

```
delivery    hop     in       out      loss     dedup  cause
metric      ingest  1275     1275     0        0      -
property    ingest  166      166      0        0      -
event       ingest  0        0        -        0      -
log         ingest  0        0        -        0      -
end to end  metric 0  property 0  event -  log -
```

The accounting has a visible cost. Against the same lab on the pre-ledger build, receipts a day went
from 56 to 129 MiB at about 2.3 batches a second (about 290 to 675 bytes a batch), database a day 309
to 371 MiB, WAL a day 725 to 851 MiB and WAL a sample 400 to 474 B. At 1,000 systems that puts the
receipt table past the scaling page's 0.5 GiB a day target; #1699 narrows the receipt or revises the
target.

Thin cuts: the induced unregistered-key end-to-end run needs a lab knob that does not exist (#1698;
the refusal is proven by the unit and bus tests), and the end-to-end test ran by hand against a
private image rather than through `make test-lab`, which rebuilds the shared dev image. Whole-batch
Terms read as `missing` until #1691, the push lane carries no ledger until #1696, and the node,
schedule and sim hops, the platform metrics and the perf gate are #1692 to #1695 and #1659.
