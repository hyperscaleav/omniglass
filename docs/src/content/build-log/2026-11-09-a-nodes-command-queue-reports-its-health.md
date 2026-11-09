---
title: "A node's command queue reports its health on its platform component (#1626)"
date: 2026-11-09
refs: [1626, 1625, 1630, 1631, 1641, 1669, 1670]
---

A command now keeps its delivery history: the dispatch stamp sets `first_dispatched_at` on the first
take and counts every take in `delivery_count`, read back over the API as `first_delivered_at` and
`delivery_count`. The console's command arc notes a redelivered command's count and its first
delivery beside its last. Every node's command queue reports itself as `command-*` platform metrics
on the node's platform component. The wire writes one dispatch latency sample at a command's first
take and one execution latency sample at its report, each in the stamp's transaction. A 15 s server
timer reads the whole fleet's queues in one statement through the same candidate fragment the pull
uses, and writes each node's pending count, oldest pending age and stuck count every read, plus the
window's redeliveries and expiries when nonzero. Expiry keys on the execution stamp rather than the
recorded status, so a fire-and-forget command no node took counts as `undelivered`
([ADR-1626](/architecture/decisions/#adr-1626-command-queue-expiry-keys-on-the-execution-stamp-not-the-status)).
The node counts its own failed command pulls once per report window on its node-self lane. Two
partial indexes serve the read, proven by an `EXPLAIN` assertion. The keys render on the collection
page from the code that declares them. The scale lab's command load
(`lab measure --command-rate`) is #1669, and the live console captures of a redelivered command and
the queue series are #1670.
