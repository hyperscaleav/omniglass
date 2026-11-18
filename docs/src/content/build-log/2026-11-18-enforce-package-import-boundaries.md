---
title: "Enforce package import boundaries"
date: 2026-11-18
refs: [2189]
---

Two guard tests in `internal/build` hold the Storage Gateway as the only production Postgres
path and keep the outpost binary free of the server DB and API layers.

`TestOnlyStorageAndToolingImportPgx` asserts that only `internal/storage` and three named
schema tools (`internal/erd`, `internal/compressbench`, `cmd/erdgen`) import
`github.com/jackc/pgx` or `database/sql` in non-test files. Any other direct import would
bypass the scope injector and writer-class invariants the gateway holds on every query.

`TestOutpostDoesNotImportStorageOrAPI` asserts that non-test files in `internal/outpost` import
neither `internal/storage` nor `internal/api`. The outpost is the edge binary; linking the
server DB or API layer into it would pull pgx and the full server stack into that build.

Both rules carry a stale-entry check: an allowlist entry that no longer holds a qualifying
import fails the test, so the list cannot silently outlive the code it covers.

To satisfy the outpost rule, `MetricTime` moved from `internal/storage/samples.go` to the new
`internal/metrictime` package. Neither the storage package nor the outpost package owns it.
`internal/outpost/spool.go` now imports `internal/metrictime` directly instead of
`internal/storage`.
