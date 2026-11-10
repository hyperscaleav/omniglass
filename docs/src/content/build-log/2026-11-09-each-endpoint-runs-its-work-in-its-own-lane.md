---
title: "Each endpoint runs its work in its own lane, operator requests first (#1249)"
date: 2026-11-09
refs: [1249, 1634, 1650, 1651, 1649]
---

Every discrete request a node makes now rides its endpoint's lane
([ADR-1249](/architecture/decisions/#adr-1249-the-endpoint-lane-runs-one-request-at-a-time-operator-work-first)):
scheduled polls and probes, commands, run nows, and previews that resolve to an attached endpoint
by transport and target. One request is in flight per endpoint while endpoints run side by side;
an operator's work goes ahead of the scheduled work queued on the lane without preempting the
request in flight. Each job asks the endpoint's gate as it is taken, so a command or run now queued
behind a reboot answers `disrupted` and sends nothing, and where several reasons hold the most
causal answers. A resolved preview is gated by its endpoint, a refusal included; an unmatched one
runs off-lane, held only by its host. Endpoints on one host share one ping per probe slot.
Operator work waiting across the lanes is capped at 64: past it a run now or preview answers 429 at
once, and the dispatch window counts `operator-offer-count` against `operator-refused-count`.
`--lane-workers` replaces `--poll-workers`. Proven over the real wire
(`TestFiftyComponentsBesideASlowDeviceRunInTheirOwnLanes`): fifty http components, one holding
each request three seconds, every fast one asked its six polls within seconds and no device ever
holding two requests at once; keying the lane on the task instead fails it. Component-wide
exclusion for a fragile device is #1649.
