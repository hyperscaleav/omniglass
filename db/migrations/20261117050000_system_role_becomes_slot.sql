-- migrate:up
-- #2061 (ADR-2032): what a system needs filled is a system slot, so role keeps
-- only its IAM sense. The rename carries the five tables that spell the system
-- sense (system_role, its assignment, type and product guards, role_choice),
-- the role_id column on the three tables that point at a slot, and every
-- constraint and index named for them (a constraint's backing index renames
-- with the constraint). The IAM role table, principal_grant.role_id and the
-- credential and oauth roles columns are the kept sense and are not touched.
-- The audit log's resource strings are history and stay as written.
--
-- The whole migration is one transaction, so it is guarded once: a database
-- that already has the system_slot table is left as it is.

DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'system_role') THEN
    RETURN;
  END IF;

  ALTER TABLE public.system_role RENAME TO system_slot;
  ALTER TABLE public.system_role_assignment RENAME TO system_slot_assignment;
  ALTER TABLE public.system_role_type RENAME TO system_slot_type;
  ALTER TABLE public.system_role_product RENAME TO system_slot_product;
  ALTER TABLE public.role_choice RENAME TO slot_choice;

  ALTER TABLE public.system_slot_assignment RENAME COLUMN role_id TO slot_id;
  ALTER TABLE public.system_slot_type RENAME COLUMN role_id TO slot_id;
  ALTER TABLE public.system_slot_product RENAME COLUMN role_id TO slot_id;

  -- Constraints, then the indexes left, on the renamed tables only.
  FOR r IN
    SELECT con.conname, cls.relname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public'
       AND cls.relname IN ('system_slot', 'system_slot_assignment', 'system_slot_type',
                           'system_slot_product', 'slot_choice')
       AND con.conname ~ '(system_role|role_choice|(^|_)role_id(_|$))'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I', r.relname, r.conname,
      regexp_replace(regexp_replace(regexp_replace(r.conname,
        'system_role', 'system_slot', 'g'), 'role_choice', 'slot_choice', 'g'),
        '(^|_)role_id(_|$)', '\1slot_id\2', 'g'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i'
       AND t.relname IN ('system_slot', 'system_slot_assignment', 'system_slot_type',
                         'system_slot_product', 'slot_choice')
       AND c.relname ~ '(system_role|role_choice|(^|_)role_id(_|$))'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      regexp_replace(regexp_replace(regexp_replace(r.relname,
        'system_role', 'system_slot', 'g'), 'role_choice', 'slot_choice', 'g'),
        '(^|_)role_id(_|$)', '\1slot_id\2', 'g'));
  END LOOP;
END $$;

-- migrate:down
DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'system_slot') THEN
    RETURN;
  END IF;

  FOR r IN
    SELECT con.conname, cls.relname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public'
       AND cls.relname IN ('system_slot', 'system_slot_assignment', 'system_slot_type',
                           'system_slot_product', 'slot_choice')
       AND con.conname ~ '(system_slot|slot_choice|(^|_)slot_id(_|$))'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I', r.relname, r.conname,
      regexp_replace(regexp_replace(regexp_replace(r.conname,
        'system_slot', 'system_role', 'g'), 'slot_choice', 'role_choice', 'g'),
        '(^|_)slot_id(_|$)', '\1role_id\2', 'g'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i'
       AND t.relname IN ('system_slot', 'system_slot_assignment', 'system_slot_type',
                         'system_slot_product', 'slot_choice')
       AND c.relname ~ '(system_slot|slot_choice|(^|_)slot_id(_|$))'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      regexp_replace(regexp_replace(regexp_replace(r.relname,
        'system_slot', 'system_role', 'g'), 'slot_choice', 'role_choice', 'g'),
        '(^|_)slot_id(_|$)', '\1role_id\2', 'g'));
  END LOOP;

  ALTER TABLE public.system_slot_product RENAME COLUMN slot_id TO role_id;
  ALTER TABLE public.system_slot_type RENAME COLUMN slot_id TO role_id;
  ALTER TABLE public.system_slot_assignment RENAME COLUMN slot_id TO role_id;

  ALTER TABLE public.slot_choice RENAME TO role_choice;
  ALTER TABLE public.system_slot_product RENAME TO system_role_product;
  ALTER TABLE public.system_slot_type RENAME TO system_role_type;
  ALTER TABLE public.system_slot_assignment RENAME TO system_role_assignment;
  ALTER TABLE public.system_slot RENAME TO system_role;
END $$;
