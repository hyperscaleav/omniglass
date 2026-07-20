-- migrate:up

-- A field_definition now draws its identity from the canonical key registry: its
-- name is a registered key, and its data_type and display_name are the key's. The
-- FK guarantees a field key is a real key. The column is nullable so a legacy field
-- (a name that predates the registry) keeps rendering; new creates set it, enforced
-- in the storage layer, not by a NOT NULL constraint that would fail the backfill.
ALTER TABLE field_definition ADD COLUMN IF NOT EXISTS key text references canonical_key (name);

-- Backfill: an existing field whose name already matches a registered key adopts it,
-- so the seeded and dev fields (serial_number, and the dev-seeded custom keys) point
-- at their key after this migration.
UPDATE field_definition fd
   SET key = fd.name
 WHERE fd.key IS NULL
   AND EXISTS (SELECT 1 FROM canonical_key ck WHERE ck.name = fd.name);

CREATE INDEX IF NOT EXISTS field_definition_key_idx ON field_definition (key);

-- migrate:down

DROP INDEX IF EXISTS field_definition_key_idx;
ALTER TABLE field_definition DROP COLUMN IF EXISTS key;
