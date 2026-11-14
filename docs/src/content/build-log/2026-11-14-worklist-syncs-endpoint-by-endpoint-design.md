---
title: "A node's worklist is designed to sync endpoint by endpoint (#1586)"
date: 2026-11-14
refs: [1586, 1577, 1583, 1872, 1623]
---

The worklist still travels as one reply, so the bus's 1 MB limit caps how much one node can run.
Grouping by endpoint (#1583) took headquarters at 3,000 systems from 1.17 MB to 463 KB gzipped,
under the limit, but a larger site crosses it again. ADR-1577's target, a materialized worklist with
one key-value entry per node, would not have helped: a key-value value is a message, held to the same
limit. ADR-1586 replaces that target. A node pulls a manifest (a version, and each endpoint's id and
content digest), is answered unchanged when it holds that version, and otherwise fetches only the
endpoints whose digest moved, in batches capped by bytes, swapping them in whole. On the soak,
headquarters runs 1,609 endpoints, so its manifest is about 64 KB gzipped, and a manifest reaches the
limit only at about 26,000 endpoints on one node. It holds no secrets at rest, so it no longer waits on
#1250. The nodes page carries the target design; nothing is built yet.
