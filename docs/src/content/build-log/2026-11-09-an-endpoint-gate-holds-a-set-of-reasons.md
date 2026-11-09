---
title: "An endpoint gate holds a set of reasons and closes when its probe reads down (#1650)"
date: 2026-11-09
refs: [1650, 1624, 1657]
---

The node's endpoint gate is now a set of reasons rather than one refusal
([ADR-1650](/architecture/decisions/#adr-1650-an-endpoint-gate-holds-a-set-of-reasons)). `refused`
keeps its rung scope and fingerprint release; `unreachable` is added when the endpoint's
reachability probe reads down twice running, dated from the first down, and removed on the next up
verdict. While it holds, a due poll is held: nothing is sent, no failure or backoff is booked, and
the report window counts it as the new `poll-held-count`. A command answers `unreachable since`
without dialing, and a run now or a preview answers 409 naming the reason instead of a 502.
`endpoint-rejection` carries every reason, the refusal first, so `rejection.Parse` and every
existing reader are unchanged, and the reachability read serves the rest as `gate_reasons`. Proven
over the real wire (`TestAGoneDeviceHoldsItsPollsUntilItReturns`): the simulated sensor goes away,
the held count rises with no failure, and when it returns on the same address its polls land within
seconds. This is the root of the gate chain: disrupted (#1651), upstream (#1652) and cross-node
(#1655) reasons are new kinds in the same table, and the console rendering the set is #1657.
