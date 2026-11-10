---
title: "The perf gate fails a lossy run and names the kind, the hop and the cause (#1695)"
date: 2026-11-10
refs: [1695, 1640, 1641, 1623, 1719]
---

`lab perf check` already held a `delivery_loss/<hop>/<kind>` exact row for every row the delivery table
prints, so a run that lost one item in a thousand at any hop already failed. What it did not do was say
why. A failed loss row now carries a note, `property lost 3 of 10 at ingest, cause missing`, naming the
kind, the hop, the items lost of the items in, and the largest outcome behind the loss: the same cause
the delivery table prints. Over several runs the worst run's note is the one shown. A failed
`delivery_unsettled_boots` row names the first three boots no ratio was read from (node and boot) and
counts the rest, with the stream's backlog note when the stream was the cause; a read that was not
measured fails as `delivery_loss` with its reason, as before. There is no tolerance.

The cause is derived from a row's outcomes rather than stored beside them. It had been a field the JSON
dropped, so `lab perf check --samples`, which reads runs back from their JSON, compared a loss it could
not explain and printed a delivery table with no cause. Reading it from the outcomes makes the table and
the gate say the same thing whether the run was just measured or read back.

The gate reads the hops the delivery table prints on this base, the ingest hop today; as the node,
schedule and outbound hops land, their per-kind rows join the gate with no change to it. The sim hop's
rows are one a protocol with no kind (#1742), so when it lands the figure name and the note must carry
the protocol, or every protocol reads as one `delivery_loss/sim/` figure. The lab guide's delivery
table and baseline sections show a failed row, and the 1640 ADR's and build log's 3000 size references
now point at #1719, which owns it.
