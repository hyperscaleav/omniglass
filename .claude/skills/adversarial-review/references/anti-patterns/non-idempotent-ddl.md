## non-idempotent-ddl (live)

- **Cue:** a migration without `IF NOT EXISTS`/`IF EXISTS` guards; a column rename
  without the catalog-guarded `DO` block; any edit to an already-applied migration file;
  seed rows inside a schema migration.
- **Failure:** partial-state databases fail mid-migrate; an edited applied migration
  silently diverges environments (dbmate keys on the version, not the contents); a
  schema squash drops the smuggled seed rows.
- **Fix:** the `/storage-schema-change` skill is the procedure; new migration files only.
