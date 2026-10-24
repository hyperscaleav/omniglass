---
title: "Every key has one writer, enforced at the gateway (#1084, #1085, #1086)"
date: 2026-10-24
refs: [1084, 1085, 1086]
---

The push lane's refusal of native keys (#1080) generalizes into an invariant the Storage Gateway
holds beside scope
([ADR-0161](/architecture/decisions/#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway)).
Every metric, property and event type has exactly one **writer class**, resolved from code and the
catalogs and never stored (`internal/writerclass`): `node-probe` (the probekeys set, which now holds
`collection-failed`), `platform` (`health`, the rollup's verdict; the self-monitoring follow-on #1094
is its planned consumer), `calc` (a calc rule's output, generalizing `WrittenBy`) and `collected`
(every other key). Every gateway function that lands a sample or an event names its writer (the node
lane bound to its task's endpoint, the push lane, a calc rule, the health rollup, a command and its
reply, an operator's declared value, a key pin (the ssh host key, the tls certificate), an event rule) and holds every row to its
key's class before anything lands; a refusal is a 422 naming the key and who writes it. The
declaration points refuse first with the same ruling: a command type's target, a driver spec's emits
(and the node's interpreter, for a task derived before), a calc rule's output, an event rule's event
types, and an operator-created type under a reserved name on any lane. On the node lane a node-probe
key lands only under the endpoint whose task the node ran; a driver's own keys keep their instance.
The reachability, session and transition-guard reads serve a key's observed series only, the health
reads the rollup's rows only, and a node-probe sample dated more than five minutes past the database's
clock is stored at the database's now. The tls
keys (#1035) are `node-probe` like every native key; the driver spec's node-only emit check #1035
added is now the writer-class ruling itself, the tls certificate's pin through `:retry` is admitted
as the ssh host key's is, and the reachability read serves a tls endpoint's pin in force (the
declared key, else the one trusted on first use). The type
reads carry `writer_class`, and the properties page renders the ruling from the code
(`cmd/writerclassgen`, `docs/src/generated/writerclass.json`). Proven test-first: a source guard reads
every sample and event insert in the gateway and fails on a function that is not a registered write
path or skips the check (shown failing on a planted unregistered path), a migration guard rules the
one historic migration that writes sample rows, a Postgres table drives every registered path with a
mismatched writer and each admitted one (`TestWriterMismatchRefused`), every seeded type reads one
class and no lane takes a reserved name (`TestEveryCatalogKeyHasOneWriterClass`), and the lanes end
to end (`TestTelemetryPushRefusesKeysItDoesNotWrite`, `TestNodeLaneConfinesNodeProbeKeysToItsEndpoint`,
`TestWriterClassOnTheWire`). Test fixtures that wrote a native key as a generic sample now write it as
the node would, under one endpoint; a few use a collected key instead (`video-input`,
`firmware-version`, `uptime` as a metric target). The dev seed writes its native samples as the node
under their endpoint, and its breadth round-trip series under one named `icmp`.
