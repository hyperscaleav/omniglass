---
title: "Every collection method runs healthy in the lab (#1370)"
date: 2026-10-29
refs: [1370, 1266, 1414]
---

Second in the collection phase of #1266. The dev fleet's HQ boardroom now runs every collection
method the rest of the fleet does not: the bar's control socket over wss (`kestrel-ws`), the
display's web admin over http digest (`boreal-edge-web`), the panel's https API behind its sign-in
form (`newtron-panel-web`), the healthy twins of the ssh and telnet faults, and a bare `icmp` probe.
A simulated device's ssh host key is now derived from its id, so a pinned key survives a simulator
restart. The simulator subscribes a simulated service to its webhook endpoint from the device list
([ADR-1370](/architecture/decisions/#adr-1370-the-simulator-plays-the-saas-so-it-holds-a-webhook-endpoints-token-and-key)),
fenced to the simulator's pull and its own components, so the UCC service's posts arrive from the
start. A simulated booking service pushes the boardroom system's `booking-count` and
`booking-state` through `POST /telemetry:push`, as a service principal holding `telemetry:push`
alone on that one system, its token minted per lab run like the node's.

`TestEveryCollectionMethodRunsOnAHealthyLabComponent` fails on a seeded driver or registry
transport no healthy simulated component exercises, and on a listed gap the fleet gains (a plain
`websocket`, #1414; the unbuilt push listener, syslog and traps). `make test-lab` proves it live: each
method up with a value its driver emits landed from it (a fanned-out one on its bound owner), and a
system holding the push.
