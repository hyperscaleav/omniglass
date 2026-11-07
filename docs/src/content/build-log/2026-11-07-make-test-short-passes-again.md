---
title: "make test-short passes again (#1542)"
date: 2026-11-07
refs: [1542, 1501]
---

The drop-at-cleanup tests from #1501 provision their database inside a subtest, so the parent can
check after the subtest's cleanup has run. Under `-short`, `NewDSN` skips that subtest, and each parent
then failed on its own guard ("the subtest did not provision a database"). Three tests did this:
`TestNewDSNDropsItsDatabaseAtCleanup`, `TestNewDSNDropsDespiteAnOpenConnection` and
`TestNewDBClosesItsPoolBeforeTheDrop`. The full gate never runs with `-short`, so CI stayed green
and only `make test-short` broke.

Each of the three now skips under `-short` before its subtest, the way every other Postgres-backed
test in `storagetest` does. Without `-short` they run and assert exactly as before.
