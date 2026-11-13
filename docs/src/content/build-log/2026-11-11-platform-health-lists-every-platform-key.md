---
title: "Platform health reads the telemetry ack floor against the stream head, and lists every platform key (#1763)"
date: 2026-11-11
refs: [1763, 1758, 1623]
---

The Queues panel gains the telemetry consumer's ack floor read against the stream's head, the pair
#1758 added for the lab's delivery read to settle on, so an operator sees the same settle the lab
waits for: the gap is the backlog in sequences, and a floor reaching an earlier head means every
batch stored by then is answered.

Both gauges had landed off the page because its panels list their keys by hand. A new test,
`TestPlatformPageListsEveryPlatformKey`, now walks every platform key the server or a node writes of
itself and fails naming any key no panel reads and the off-page list does not name. That list
gives each kept-off key its reason (a process's runtime figures, the bus's request and pull counts,
the retention pass's bookkeeping, node detail beyond the table's columns), all of which read on the
component page a panel or a row opens. Before the panel change the test failed naming exactly the
two new gauges.
