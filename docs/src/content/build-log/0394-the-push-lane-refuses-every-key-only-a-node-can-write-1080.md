---
title: "The push lane refuses every key only a node can write (#1080)"
date: 2026-10-23
refs: [1080]
---

`POST /telemetry:push` refused a node's control facts (the gate, the trusted host key, a held
session's state and reason) but took the rest of the native probe keys like any property, so a
principal holding `telemetry:push` could write a reachability verdict, its rungs, or a probe metric
or timing under an endpoint's name, and the console read it as the node's observation. Now every
native key is refused by name, on every lane (a native name pushed as an event is refused as the
node's, not as unregistered), under any instance and any casing, beside rows in the same batch that
land; the rejection's reason names the key. The set is `internal/collection/probekeys` itself: each
key is ruled there node-only or pushable with the one-line reason it may be pushed (none is
pushable today), and a test reads the package's declared constants from source and fails on a key
ruled neither, so a key declared there cannot ship pushable by omission; the reachability read's
keys are now those declarations too, and a test fails on one the ruling does not hold node-only. The
node's own lane never consults the ruling. Proven against Postgres and a real bus (`TestTelemetryPushRefusesNativeKeys`:
every node-only key on every lane and instance refused by name, a batch landing only its two
pushable rows, casing and spacing variants refused, nothing native landing;
`TestNodeLaneLandsEveryNativeKey`: a node publishing every native key on its own subject lands
each one), and in unit tests (`TestEveryKeyRuled`, `TestIsNodeOnly`, `TestNodeOnlyRejection`,
`TestReachabilityReadsOnlyNodeOnlyKeys`). The review found other write surfaces that can reach a
native key's series, and node-lane confinement wider than the endpoint; they are #1084 and #1085,
and `collection-failed`, a node-emitted key declared outside `probekeys`, is #1086.
ADR-0158 point 5 is amended; the push tests that used `icmp-rtt-avg` as their sample metric now
push `room-temperature` and `uptime`.
