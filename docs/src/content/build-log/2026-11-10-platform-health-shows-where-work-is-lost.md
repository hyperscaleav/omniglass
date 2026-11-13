---
title: "Platform health shows where work is lost, hop by hop (#1646)"
date: 2026-11-10
refs: [1646, 1659, 1641, 1644, 1692, 1693]
---

Platform health draws the path a collected item takes from its run to the store as hops, the node's
spool then the server's ingest, each read from the three delivery keys #1659 writes on the platform
components that run it. `storage.PlatformPipelineHops` declares the hops and one read composes them:
each hop's in, out and loss per kind over the newest window and the last hour, each component's
share, and the end to end, with nothing in reading no ratio rather than 0. The platform view serves
it as `pipeline`, with each node's composed `loss` and the hops it covers in `loss_hops`. The console
draws it as `PipelineFlow`: the hops with a kind filter, a tint above 0, and an explainer per hop
naming its keys and linking each component to its series; the node table gains a Node Loss column.
The walkthrough `find-where-work-is-lost` breaks a simulated panel and shows its failed polls landing
on the panel while the node hop stays at 0. The walkthrough lab now seeds a 10 s report window so its
figures land inside the recording. The schedule hop (#1693) and the node's causes (#1692) are not
drawn yet. The decisions are
[ADR-1646](/architecture/decisions/#adr-1646-where-work-is-lost-is-a-composed-read-of-the-delivery-keys-hop-by-hop).
