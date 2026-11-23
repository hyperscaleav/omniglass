---
title: "The boot audit names dialed endpoints stored before the target check (#2249)"
date: 2026-11-23
refs: [2249, 1187]
---

Bare dialed endpoints stored before the write-time target check (#1187) can hold `host` and `port`
keys, no `target`, or a `target` with no port on a transport that has no default. The outpost skips
such a task forever, so the endpoint's reachability never reads and nothing said why.

Following ADR-1100's pattern for an invariant that only gates new writes, the boot audit now runs
`checkTarget` over every stored bare dialed endpoint that has params and logs each one that fails,
naming the endpoint, its transport and the fix. Rows that pass are untouched, and so are failing
ones: the audit only names them (a rewrite could not know the intended address). Regression test:
`TestStoredEndpointTargetViolations` in `internal/storage`. An endpoint created with no params at
all stays #2228.
