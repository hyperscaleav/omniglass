-- migrate:up
-- #2070 (ADR-2035): the rows the fleet is typed by are the catalog; a
-- registry is a compiled-in lookup. The fork shadow table spells the old word
-- in its name, its key column and every constraint named for them (the
-- primary key and the named not-null constraints), so all take the new one. Rows carry over untouched: the key column holds a catalog table's name
-- (component_type, location_type), which does not change.
--
-- Guarded once: a database that already has catalog_shadow is left as it is.

DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'registry_shadow') THEN
    RETURN;
  END IF;
  ALTER TABLE public.registry_shadow RENAME TO catalog_shadow;
  ALTER TABLE public.catalog_shadow RENAME COLUMN registry TO catalog;
  FOR r IN
    SELECT conname FROM pg_constraint WHERE conrelid = 'public.catalog_shadow'::regclass
       AND conname ~ 'registry'
  LOOP
    EXECUTE format('ALTER TABLE public.catalog_shadow RENAME CONSTRAINT %I TO %I',
      r.conname, replace(r.conname, 'registry', 'catalog'));
  END LOOP;
END
$$;

-- migrate:down
DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'catalog_shadow') THEN
    RETURN;
  END IF;
  FOR r IN
    SELECT conname FROM pg_constraint WHERE conrelid = 'public.catalog_shadow'::regclass
       AND conname ~ 'catalog'
  LOOP
    EXECUTE format('ALTER TABLE public.catalog_shadow RENAME CONSTRAINT %I TO %I',
      r.conname, replace(r.conname, 'catalog', 'registry'));
  END LOOP;
  ALTER TABLE public.catalog_shadow RENAME COLUMN catalog TO registry;
  ALTER TABLE public.catalog_shadow RENAME TO registry_shadow;
END
$$;
