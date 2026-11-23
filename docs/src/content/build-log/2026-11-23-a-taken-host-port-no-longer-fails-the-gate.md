---
title: "A taken host port no longer fails the Go gate (#2358)"
date: 2026-11-23
refs: [2358, 1845]
---

On a busy shared host, rootless Docker could hand a Postgres testcontainer a host port another
process bound first, and the start failed with `address already in use`, so the gate went red on
code that never touched Go. The shared start path now retries a start that fails that way, up to
four attempts, terminating the failed attempt's container each time. Any other start error still
fails at once. The retry is `storagetest.RunWithPortRetry`, used by the harness and by the
`labfootprint` psql test, with the decision (`isPortCollision`) unit-tested.
