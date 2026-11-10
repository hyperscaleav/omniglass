---
title: "Where work is lost is a composed read of the delivery keys, hop by hop"
date: 2026-11-10
status: "Accepted"
summary: "Accepted: Platform health draws the pipeline as the hops a declared table in storage names (node, then ingest), each read from the three delivery keys on the platform components of its class, its loss (in - out) / in over the summed counts and the end to end 1 - prod(1 - loss). A node's Loss is a composed view field over the hops its own component records, not a key group. The schedule hop (#1693) and the node's causes (#1692) are not drawn until their figures are written. The walkthrough lab's seed sets the report window to 10s."
refs: [1646, 1659, 1641, 1644, 1623, 1692, 1693]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [Platform health](/guides/operator/platform-health/#where-work-is-lost), [messaging](/architecture/messaging/#measuring-the-pipeline), [api](/architecture/api/)
- **Decision:** (#1646, under #1623.)
  1. **The hops are a declared table, read from one key set.** `storage.PlatformPipelineHops`
     names each hop in pipeline order with its label, what counts as out and lost there, the class of
     platform component that records it (node or server) and the instance form that selects its
     series. The read selects `delivery-in-count`, `delivery-out-count` and `delivery-loss-ratio`
     on every platform component in one statement, and a hop's rows are its class's series at its
     instance form. A new hop is a table row and the keys its component writes, not a new read.
  2. **One loss rule, composed from counts.** A hop's loss is `(in - out) / in` over its
     components' summed counts (the newest window and the last hour), never an average or sum of
     their ratios; the end to end per kind is `1 - prod(1 - loss)` over the hops that kind crossed
     (`delivery.EndToEnd`). Nothing in is no ratio, and a kind with no series at a hop is no row:
     neither reads 0. A component's newest window counts toward the newest figures only while it
     is current, within two report windows (`platformWindowHorizon`, the horizon a window count
     already reads by): a node that stopped, or a server back under a new name, keeps its last
     window as its newest sample forever, and that window counts in the hour, not as now.
  3. **A node's Loss is a composed field, not a key group.** The issue placed the node's delivery
     as a column group of keys. The page shows one figure per node instead, the end to end over the
     last hour of the hops its own component records, every kind summed, and the view says which
     hops in `loss_hops`, so the page names them without assuming. The per-kind series stay on the
     node's platform component, one click away.
  4. **The node hop is #1659's spool figures.** It is drawn from the keys #1659 writes at the
     spool, before the node's causes of loss by failure (#1692) exist, so it reads in, out and lost,
     not why.
  5. **Hops with no figures are not drawn.** The schedule hop (whether each run the worklist asks
     for is run, #1693) writes no delivery keys yet, so the path starts at the node's spool; it is
     not drawn as a hop reading 0, which would claim a measurement that was not made.
  6. **The walkthrough lab reports every 10 s.** A lab started for a recording (`walkthrough`
     cadence) with its node seeds `collection.report_seconds` to 10, the setting's floor, through the
     new hidden `lab seed --report-seconds`, so the first delivery and poll figures land well inside
     a minute's recording. A learner's lab and a lab with no node (the docs shots) keep the
     platform's 60 s.
- **Consequences:** the [lab topology](/guides/lab/) table gains the report window column. The
  server ingest hop reads a small nonzero loss on a fresh lab while the node hop reads 0 (#1641),
  which this page now makes visible rather than fixes.
