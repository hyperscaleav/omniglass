---
title: "A device nothing has collected from reads the same everywhere (#1454)"
date: 2026-11-14
refs: [1454, 1453]
---

A device with an ICMP endpoint and no node read `unknown` in the Explore drawer, `healthy` on its page,
and carried a `node offline` chip beside an outlined `reachable`. The component's verdict now says
`unknown` until an alarm was raised on it or an endpoint delivered (the alarms read, the fleet view's dots
and a system's members share one expression), `collectionState` gives a missing node its own `no-node`
state instead of `node-offline`, and the Collection row shows the reachability ladder only once a rung has
an observation. An e2e test drives the alarms read through never observed, alarmed, cleared and delivered;
the unit and page tests pin the no-node state and the absent chip. The decision is ADR-1454. The enroll
pointer for the no-node state is #1453's.
