---
title: "Import boundary guard: db packages stay in the Storage Gateway (#2189)"
date: 2026-11-18
refs: [2189]
---

Adds `TestImportBoundaries` in `internal/build/` to enforce two architecture invariants by
reading the module's import graph with `go list -json ./...`:

1. Only `internal/storage`, its `storagetest` helpers, and named tooling may import
   `github.com/jackc/pgx` or `database/sql`. Any package that bypasses the Storage Gateway
   fails the test with its name.

2. `internal/outpost` production code must not import `internal/storage` or `internal/api`.
   The outpost is an edge binary; coupling it to the server graph inflates the binary and
   ties its release to the server's.

Each rule carries a stale-entry check: an allowlist entry whose package no longer imports
the controlled dependency fails the test, so the list cannot quietly grow stale.

To satisfy rule 2, `MetricTime` (the millisecond-precision timestamp rounding shared by the
outpost's spool and the storage write path) moves from `internal/storage/samples.go` to
`internal/metric/time.go` as `metric.Time`. Both packages import the new, dependency-free
`internal/metric` package; `internal/outpost` no longer imports `internal/storage`.

The Storage Gateway doctrine page in the architecture docs gains a Package import boundaries
section listing both rules and pointing to the test.
