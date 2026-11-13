---
title: "Every hop writes its delivery loss per kind on its platform component (#1659)"
date: 2026-11-10
refs: [1659, 1641, 1623, 1625, 1628, 1644, 1646]
---

The node and the server each write their own hop's delivery as three platform keys,
`delivery-in-count`, `delivery-out-count` and `delivery-loss-ratio`, with the item kind (metric,
property, event, log) as the instance, every report window and 0 when quiet. A new
`internal/delivery` `Flow` counts items as they settle and renders a window's readings; the node's
spool counts an acked batch in and out and a dropped, refused or unencodable one in only, and the
server's consumer counts each landing's store outcome, each held redelivery as in and out, and each
Termed batch as in only. The keys replace `spool-drop-count`, `publish-failure-count` and
`bus-telemetry-drop-count`, which are gone from the seed, the writer class and the platform view's
interim delivery group; the API lane's refused samples now count as lost too. An unbound identity
(one no binding places, which the conduit's inbox already surfaces) now counts as accounted for, not
loss, on the server hop and in the lab's ledger read, so the dev seed's unassigned room reads 0. The decisions are
[ADR-1659](/architecture/decisions/#adr-1659-a-hop-writes-its-delivery-in-out-and-loss-ratio-per-kind-every-window).
