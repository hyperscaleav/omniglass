---
title: "Migration versions are unique and newer than main's (#1246)"
date: 2026-10-26
refs: [1246, 933]
---

dbmate keys an applied migration on its version alone, so a hand-picked version is a collision
waiting to happen: #933 had two branches each take `20261019050000`, and whichever merged second
would have been recorded as applied without running. Two checks now hold the rule. `make test`
fails when two files in `db/migrations/` share a version or a file carries no `dbmate new`
stamp (`db.CheckUnique`). The new `migrations` workflow, and `make migration-check` locally,
fails a change whose added migration is not newer than the newest on the base branch's tip, or
is stamped later than the clock (`db.CheckAgainstBase`, run by `cmd/migrationcheck` against the
base read from git). It runs on each PR and on each push to `main`, where the base is the commit
before the push, so two PRs that both went green before either merged still meet the check.

`make migration NAME=<snake_case_name>` wraps `dbmate new` at the version `go.mod` pins, so a
version is a UTC stamp, not a choice. The `/storage-schema-change` skill,
the [storage](/architecture/storage/) page and [release and CI](/contributing/release-ci/)
teach it.
