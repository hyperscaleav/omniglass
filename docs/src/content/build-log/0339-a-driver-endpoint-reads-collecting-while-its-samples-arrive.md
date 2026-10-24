---
title: "A driver endpoint reads collecting while its samples arrive (#933)"
date: 2026-10-20
refs: [933]
---

A component's Collection card read **no sample yet** on a driver endpoint (an SNMP display, say) while
its Vitals showed a live Uptime: the card judged collection by the reachability verdict, which only a
probe endpoint gets. No sample row can stand in for it: ingest stamps a sample with its endpoint's name
only when the driver gave it no instance of its own, the property lane writes only a changed value, and
a sample's time is the node's clock. So ingest now records it directly: as it lands a batch from a task,
it stamps the task's endpoint seen in `endpoint_live`, by the server's clock, whatever the batch carried.
The stamp is coalesced as `event_rule_live` is: one statement per batch, a row stamped within 5s left
alone, a row another batch is stamping skipped (SKIP LOCKED), and a first sight inserted ON CONFLICT DO
NOTHING so two batches seeing a new endpoint never abort each other. The reachability read carries it as
`last_seen` per endpoint, and the verdict's `ts` is documented for what it is, the last change. The console
judges collection by `last_seen` (a down verdict still reads as the component answering badly), so a
driver endpoint reads collecting while its batches land and stale once they stop. Proven by a bus test
through the one ingest write path (an instanced driver series, a property-only driver whose value never
changes, a sample dated an hour ahead), a storage test (first sight, the coalescing bound, sixteen
concurrent first sights), the reachability API test, and pure tests of the collection state.
