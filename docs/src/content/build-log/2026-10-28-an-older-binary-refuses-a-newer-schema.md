---
title: "An older binary refuses a newer schema (#1289)"
date: 2026-10-28
refs: [1289, 1287]
---

An image older than its database used to boot silently: `migrate.Run` applied whatever was pending
and the server came up on columns it did not know. A `helm rollback` past a migration was the
common way to get there.

Every migrate now starts with a guard. It reads every version the database records as applied,
including ones the binary does not embed (which dbmate's own listing drops), and compares the newest
with the newest embedded migration. A newer database stops the `migrate` run mode, the server's boot
and every direct-database command with `ErrSchemaAhead`, naming both versions. A database at the
binary's head or behind it migrates as before, and a version below the head that a collapsed chain
(#388) no longer embeds passes, since it is history rather than a newer schema.

`TestRunRefusesASchemaNewerThanTheBinary` stamps a migration past the embedded head into a real
Postgres and proves the refusal names both versions; with the guard removed it fails. This is the
schema-guard member of the Helm epic (#1287).
