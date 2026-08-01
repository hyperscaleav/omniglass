-- migrate:up
-- #460, two schema-hygiene items with zero API surface.
--
-- 1. `declared` is impossible on the sample sinks: a declared value is an
-- operator assertion and lives only in the property latest-value cache; the
-- sample writers only ever receive observed or calculated (plus intended via
-- the command pillar). Narrow the provenance domains and drop the matching
-- dead lineage arm so no dead SQL survives the narrowing. No existing rows
-- can violate this (no writer ever produced 'declared' sample rows).
ALTER TABLE metric DROP CONSTRAINT IF EXISTS metric_provenance_check;
ALTER TABLE metric ADD CONSTRAINT metric_provenance_check
    CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text])));
ALTER TABLE metric DROP CONSTRAINT IF EXISTS metric_lineage_check;
ALTER TABLE metric ADD CONSTRAINT metric_lineage_check
    CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL)) OR ((provenance = 'intended'::text) AND (event_id IS NOT NULL) AND (source_rule IS NULL))));
ALTER TABLE state DROP CONSTRAINT IF EXISTS state_provenance_check;
ALTER TABLE state ADD CONSTRAINT state_provenance_check
    CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text])));
ALTER TABLE state DROP CONSTRAINT IF EXISTS state_lineage_check;
ALTER TABLE state ADD CONSTRAINT state_lineage_check
    CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL)) OR ((provenance = 'intended'::text) AND (event_id IS NOT NULL) AND (source_rule IS NULL))));

-- 2. The uuid-refactor constraint-name collision on role: the NAME column's
-- NOT NULL is called role_id_not_null and the real id column got the
-- digit-suffixed fallback. Rename in dependency order (freeing the id name
-- first requires moving the name-column constraint), guarded because
-- RENAME CONSTRAINT has no IF EXISTS.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class rel ON rel.oid = con.conrelid
             WHERE rel.relname = 'role' AND con.conname = 'role_id_not_null'
               AND NOT EXISTS (SELECT 1 FROM pg_constraint c2 JOIN pg_class r2 ON r2.oid = c2.conrelid
                               WHERE r2.relname = 'role' AND c2.conname = 'role_name_not_null')) THEN
    ALTER TABLE role RENAME CONSTRAINT role_id_not_null TO role_name_not_null;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class rel ON rel.oid = con.conrelid
             WHERE rel.relname = 'role' AND con.conname = 'role_id_not_null1') THEN
    ALTER TABLE role RENAME CONSTRAINT role_id_not_null1 TO role_id_not_null;
  END IF;
END $$;

-- migrate:down
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class rel ON rel.oid = con.conrelid
             WHERE rel.relname = 'role' AND con.conname = 'role_id_not_null'
               AND EXISTS (SELECT 1 FROM pg_constraint c2 JOIN pg_class r2 ON r2.oid = c2.conrelid
                           WHERE r2.relname = 'role' AND c2.conname = 'role_name_not_null')) THEN
    ALTER TABLE role RENAME CONSTRAINT role_id_not_null TO role_id_not_null1;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint con JOIN pg_class rel ON rel.oid = con.conrelid
             WHERE rel.relname = 'role' AND con.conname = 'role_name_not_null') THEN
    ALTER TABLE role RENAME CONSTRAINT role_name_not_null TO role_id_not_null;
  END IF;
END $$;
ALTER TABLE metric DROP CONSTRAINT IF EXISTS metric_provenance_check;
ALTER TABLE metric ADD CONSTRAINT metric_provenance_check
    CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text])));
ALTER TABLE metric DROP CONSTRAINT IF EXISTS metric_lineage_check;
ALTER TABLE metric ADD CONSTRAINT metric_lineage_check
    CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL)) OR ((provenance = 'intended'::text) AND (event_id IS NOT NULL) AND (source_rule IS NULL)) OR ((provenance = 'declared'::text) AND (source_rule IS NULL) AND (event_id IS NULL))));
ALTER TABLE state DROP CONSTRAINT IF EXISTS state_provenance_check;
ALTER TABLE state ADD CONSTRAINT state_provenance_check
    CHECK ((provenance = ANY (ARRAY['observed'::text, 'calculated'::text, 'intended'::text, 'declared'::text])));
ALTER TABLE state DROP CONSTRAINT IF EXISTS state_lineage_check;
ALTER TABLE state ADD CONSTRAINT state_lineage_check
    CHECK ((((provenance = 'observed'::text) AND (event_id IS NULL)) OR ((provenance = 'calculated'::text) AND (source_rule IS NOT NULL) AND (event_id IS NULL)) OR ((provenance = 'intended'::text) AND (event_id IS NOT NULL) AND (source_rule IS NULL)) OR ((provenance = 'declared'::text) AND (source_rule IS NULL) AND (event_id IS NULL))));
