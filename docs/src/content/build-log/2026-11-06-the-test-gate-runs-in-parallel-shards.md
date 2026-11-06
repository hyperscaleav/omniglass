---
title: "The test gate runs in parallel shards and reports its slowest tests (#1536)"
date: 2026-11-06
refs: [1536, 214]
---

The `test` job took 14 to 17 minutes, and because main requires branches to be up to date, that set
the pace of the whole merge queue. `test.yml` now runs the parts of `make test` as parallel jobs:
`test-web`, three Go shards, and `test-netadmin`. The shards are Makefile targets, so a contributor
runs exactly what CI runs (#214): `test-go-storage` and `test-go-devseed` (the two slowest packages,
510 s and 475 s on the baseline run) and `test-go-rest`, whose package list is `go list ./...` minus
the named shards, never hand-listed. `make test-go-shards-check` fails if any package is in no shard
or in two, and every shard target runs it first. `make test` still runs everything, serially, with the
same `TEST_PARALLEL` default of 1.

The required check is unchanged: a final job named `test` `needs` every shard, runs `if: always()`,
and fails unless each succeeded, so the ruleset is untouched.

Each Go shard runs under the new `cmd/gotestreport`, which runs `go test -json`, prints what a plain
`go test` would (an `ok` line per passing package, the failing tests' output for a failing one), and
ends the log with the 20 slowest top-level tests and each package's wall time. That list is where
the follow-up that makes the suite itself faster starts.
