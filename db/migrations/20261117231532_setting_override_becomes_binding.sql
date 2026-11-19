-- migrate:up
-- #2079 (ADR-2037): a value set at one cascade level is a binding that a
-- deeper one shadows; override is the catalog's sparse edit alone. The rename
-- carries the settings table and every constraint and index named for it (a
-- constraint's backing index renames with the constraint).
--
-- The whole migration is one transaction, so it is guarded once: a database
-- that already has the setting_binding table is left as it is.

DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'setting_override') THEN
    RETURN;
  END IF;

  ALTER TABLE public.setting_override RENAME TO setting_binding;

  FOR r IN
    SELECT con.conname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND cls.relname = 'setting_binding'
       AND con.conname ~ 'setting_override'
  LOOP
    EXECUTE format('ALTER TABLE public.setting_binding RENAME CONSTRAINT %I TO %I', r.conname,
      replace(r.conname, 'setting_override', 'setting_binding'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i' AND t.relname = 'setting_binding'
       AND c.relname ~ 'setting_override'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      replace(r.relname, 'setting_override', 'setting_binding'));
  END LOOP;
END $$;

-- migrate:down
DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'setting_binding') THEN
    RETURN;
  END IF;

  FOR r IN
    SELECT con.conname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND cls.relname = 'setting_binding'
       AND con.conname ~ 'setting_binding'
  LOOP
    EXECUTE format('ALTER TABLE public.setting_binding RENAME CONSTRAINT %I TO %I', r.conname,
      replace(r.conname, 'setting_binding', 'setting_override'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i' AND t.relname = 'setting_binding'
       AND c.relname ~ 'setting_binding'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      replace(r.relname, 'setting_binding', 'setting_override'));
  END LOOP;

  ALTER TABLE public.setting_binding RENAME TO setting_override;
END $$;
