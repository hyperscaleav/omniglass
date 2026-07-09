-- migrate:up

-- Rename the soft-delete marker from deactivated_at to archived_at (issue #146):
-- the lifecycle verb for the soft delete is now ARCHIVE (paired with RESTORE),
-- distinct from DISABLE (the reversible suspend on the `active` flag), so the
-- names read pause -> remove -> destroy. Postgres has no RENAME COLUMN IF EXISTS,
-- so the rename is catalog-guarded to stay idempotent on partial state.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'principal' AND column_name = 'deactivated_at')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'principal' AND column_name = 'archived_at') THEN
    ALTER TABLE principal RENAME COLUMN deactivated_at TO archived_at;
  END IF;
END $$;

-- migrate:down

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'principal' AND column_name = 'archived_at')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'principal' AND column_name = 'deactivated_at') THEN
    ALTER TABLE principal RENAME COLUMN archived_at TO deactivated_at;
  END IF;
END $$;
