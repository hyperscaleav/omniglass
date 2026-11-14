---
title: "A node's worklist is designed to sync endpoint by endpoint (#1586)"
date: 2026-11-14
refs: [1586, 1577, 1583, 1584, 1872, 1623]
---

The worklist still travels as one reply, so the bus's 1 MB limit caps how much one node can run.
Grouping by endpoint (#1583) took headquarters on a 3,000-system seed from 1.17 MB to 463 KB
gzipped, under the limit, but a larger site crosses it again. ADR-1577's target, a materialized
worklist the server writes into a per-node bucket on change, would lift the limit too, but only with
a mark in every writer that changes a worklist, a reconcile, and resolved secrets at rest (#1250).
ADR-1586 replaces that target with a sync that adds no state. A node pulls a manifest (its config
generation, a set digest, and each endpoint's id and content digest), is answered unchanged when it
holds that set, and otherwise fetches only the endpoints whose digest moved, in batches capped by
bytes, applying them as a set once it holds every endpoint listed. On the corporate soak headquarters
runs 1,609 endpoints, so its manifest is about 100 KB gzipped, and a manifest reaches the limit at
about 16,800 endpoints on one node. Until #1584 a steady-state pull still reads on the server. The
nodes and messaging pages carry the target design; nothing is built yet.
