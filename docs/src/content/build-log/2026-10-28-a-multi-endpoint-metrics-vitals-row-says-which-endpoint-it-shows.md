---
title: "A multi-endpoint metric's vitals row says which endpoint it shows (#1020)"
date: 2026-10-28
refs: [1020, 456]
---

A component's Vitals row for a metric two endpoints collect showed the newest sample across them
with no word of which endpoint, or that another existed. Beside a calc rule's max over the same key
it read as a contradiction. The metrics read now names the instance its value came from, counts the
live instances holding a sample, and lists each one's latest when there is more than one. The
console's row reads "latest of 2 endpoints (tcp)", with a tooltip, and opens to each endpoint's
value and age. A single-instance metric reads as before.

The read also stopped showing a deleted endpoint's frozen reading. The ADR-0456 liveness test,
which only the per-key reads (reconciliation, settlement) applied, is now one SQL predicate,
`liveInstanceSQL`, that this read applies too, so a metric whose only observer was deleted leaves
the read. The properties side needed nothing new: since #1297 each row's reconciliation already names
its `is_instance`. `TestEffectiveMetricsNameTheShownInstance` covers two endpoints, a newer sample
moving the shown one, an endpoint retired, a calculated owner-level sample, and a system's pushed
metric.
