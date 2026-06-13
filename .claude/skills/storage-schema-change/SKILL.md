---
name: storage-schema-change
description: Use when adding or altering an omniglass storage schema element (a new column, a new table, or a rename). Covers how dbmate migrations work (run-once, never edited after applied, embedded), idempotent DDL (incl the Postgres rename DO-block, since PG has no RENAME COLUMN IF EXISTS), the three migration buckets, the Storage Gateway ripple, and the testcontainer round-trip RED->GREEN.
---

# Storage schema change

## How dbmate migrations work (read first)

- A migration is **pure DDL** in `db/migrations/YYYYMMDDHHMMSS_<name>.sql`, embedded into
  the binary (`//go:embed`) and applied by the `migrate` run mode (`omniglass migrate`).
- **They run exactly once.** dbmate records each applied version in a `schema_migrations`
  table and never re-runs it. The timestamp prefix is the identity (dbmate orders lexically),
  **not** the file contents.
- **Never edit a committed/applied migration.** Because dbmate keys on the version, editing
  an applied file changes nothing on an existing database and silently diverges environments.
  To change schema, **add a new migration**.
- Every migration has a `-- migrate:up` and a `-- migrate:down`; mirror the up in the down.
- **Three buckets, never conflated** (see CLAUDE.md): schema migrations are pure DDL with
  **no seed rows** (a schema dump/squash drops them); ship-with reference data goes in the
  boot seed phase (idempotent upsert); transforming existing operator data is a one-time
  backfill migration.

## Writing the migration (idempotent)

Existing databases may have partial state, so DDL is idempotent:

- add column: `ALTER TABLE t ADD COLUMN IF NOT EXISTS c ...`
- new table: `CREATE TABLE IF NOT EXISTS ...`
- **rename column** (PG has *no* `RENAME COLUMN IF EXISTS`): a catalog-guarded `DO` block,
  mirrored in `-- migrate:down`:
  ```sql
  DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='t' AND column_name='old')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='t' AND column_name='new') THEN
      ALTER TABLE t RENAME COLUMN old TO new;
    END IF;
  END $$;
  ```

## The Gateway ripple

The Storage Gateway is the only door to the database, so a schema change ripples up:

- Update the gateway struct + JSON tags (the JSON tag is the API/CLI wire contract; keep it
  consistent with the column).
- A new `Gateway` interface method must be implemented on **every** gateway implementation
  and test double in the repo, or dependent packages fail to compile. Add it symmetrically.
- PG impl: marshal maps to `jsonb` via `$n::jsonb`; scan `jsonb` into `[]byte` then
  `json.Unmarshal`; nil map -> `{}`. Not-found wraps `pgx.ErrNoRows`.

## RED -> GREEN

- Write the **testcontainer** storage round-trip test first (insert/register -> read back,
  including upsert + not-found). It compile-fails until the API exists = clean RED.
  testcontainers gives each run an ephemeral Postgres on a **random** host port (never bind a
  fixed port).
- Implement struct + interface + PG impl (+ doubles) + migration -> GREEN.
- `go test ./...` (full, real PG) **and** `make test-short` green; then commit.
