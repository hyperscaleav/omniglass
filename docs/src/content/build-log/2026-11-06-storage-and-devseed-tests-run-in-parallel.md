---
title: "The storage and devseed tests run in parallel, and devseed seeds once (#1537)"
date: 2026-11-06
refs: [1537, 1536]
---

The first run of the sharded gate (#1536) put `internal/storage` at 294 s and `internal/devseed` at
281 s, the two slowest packages. The cause differed between them.

`internal/storage` ran 1,060-odd tests one after another: nothing opted into `t.Parallel()`, so the
package's wall time was the sum of its tests although each owns an isolated database. Every top-level
test now starts with `t.Parallel()` and shares the package's one container. The few that touch
process-wide state (the `Set*Cap` hooks, the slog default, `rules.CompileCount`) are marked
`// Serial:` and run before the parallel tests are released. Three `pg_stat_activity` queries now
filter on `current_database()` so one test no longer sees another's sessions, and the container runs
with `fsync=off` and `max_connections=400`.

`internal/devseed` paid for the boot seed and a full devseed Run in each of about twenty tests. The
harness gained derived templates (`storagetest.NewDSNFrom`): the seed runs once per binary into a
sealed template and each test clones it. Tests that verify seeding (idempotence, the port option, the
large scenario) still seed. The large scenario is real work and cannot be cloned, so it moved to
`budget_test.go`, which sorts first and is therefore released first among the parallel tests.

A scanner, `storagetest.RequireParallelOrSerial`, runs as a test in both packages and fails any test
that is neither parallel nor marked serial. No test was removed, skipped or moved behind `-short`.
