---
title: "The api and node tests run in parallel (#1540)"
date: 2026-11-07
refs: [1540, 1537, 1536]
---

With storage and devseed parallel (#1537), the `test-go-rest` shard was bound by `internal/api` at
177.5 s and `internal/node` at 159.3 s. The two had different causes.

`internal/api` ran about 350 tests one after another, each paying roughly 170 ms of setup: a 55 ms
database clone and a 115 ms boot seed. The wall time was the sum of many short tests plus the same
seed repeated. Every top-level test now starts with `t.Parallel()`, and about two hundred of them
clone a derived template that holds the booted seed (`seededDSN`, built once per binary with
`storagetest.NewDSNFrom`) instead of seeding themselves.

`internal/node` was dominated by a few long end-to-end tests: nine tests over five seconds summed to
88.8 s, and thirteen between one and five seconds to 41.7 s. Running them in parallel overlaps those
waits. Their stacks already bind ephemeral ports (`sim.EphemeralPorts`, `bus.Config{Port: -1}`,
`httptest`), so nothing needed to change but the `t.Parallel()` call.

Six tests swap `slog.Default` or assert on a package log and are marked `// Serial:`: the webhook
ingress end-to-end test in api, and the self-log, UDP datagram, TLS probe and two spool tests in node.
`storagetest.RequireParallelOrSerial` runs as a test in both packages so a new serial test must say
why. No test was removed, skipped or moved behind `-short`.
