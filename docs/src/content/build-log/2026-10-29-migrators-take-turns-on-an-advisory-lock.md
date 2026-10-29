---
title: "Migrators take turns on an advisory lock (#1288)"
date: 2026-10-29
refs: [1288, 1287, 1289]
---

dbmate takes no lock by default, so two servers starting after one upgrade raced to apply the same
pending migrations and all but one failed on a duplicate object. A two-replica Helm release did
exactly that in its `migrate` init containers.

`migrate.Run` now holds a Postgres advisory lock (`migrate.LockKey`) from the schema guard through
its last migration, on one pinned connection, and turns on dbmate's own migration lock for the
migrating connection as a second fence. The first run migrates; the rest wait (up to five minutes),
then judge the schema the first one left and find nothing to do. Holding the guard inside the lock
closes the window #1289 left: a run that waited now refuses a schema a newer binary moved while it
waited, rather than booting on it.

`TestConcurrentRunsApplyEachMigrationOnce` races four runs at an empty database and fails without the
lock on a duplicate catalog key. `TestRunGuardsTheSchemaUnderTheLock` holds the lock, lets a run queue,
stamps a future migration and releases; the run must return `ErrSchemaAhead`. Whether the chart runs
migrations as a Helm hook Job or keeps the per-replica init container is still open on #1288; the
lock is the guarantee either way.
