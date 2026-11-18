-- migrate:up
-- #2059 (ADR-2031): the collecting process is an outpost, not a node. The
-- rename carries everything in the schema that spells the old noun: the two
-- tables and their identity sequence, every node_id column, telemetry_receipt's
-- node and node_loss, every constraint and index named for it, the 'node'
-- owner kind and principal kind (checks and rows), role permission nouns, and
-- the seeded platform rows the boot seed keys by name (so the seed's next
-- upsert lands on the renamed row rather than beside it). endpoint.node_name,
-- which holds the collecting node's principal id rather than a name, becomes
-- endpoint.outpost_id, the name every other reference to the outpost carries.
--
-- The whole migration is one transaction, so it is guarded once: a database
-- that already has the outpost table is left as it is.

DO $$
DECLARE
  r record;
  defs text[] := '{}';
  i int;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'node') THEN
    RETURN;
  END IF;

  ALTER TABLE public.node RENAME TO outpost;
  ALTER TABLE public.node_log RENAME TO outpost_log;
  ALTER SEQUENCE public.node_log_id_seq RENAME TO outpost_log_id_seq;

  ALTER TABLE public.endpoint RENAME COLUMN node_name TO outpost_id;
  ALTER TABLE public.endpoint RENAME CONSTRAINT endpoint_node_name_fkey TO endpoint_outpost_id_fkey;
  ALTER INDEX public.endpoint_node_name_idx RENAME TO endpoint_outpost_id_idx;

  -- Every column spelling the noun as a word: node_id, collector_node_id,
  -- node, node_loss. metric_series' generated owner_id follows its column.
  FOR r IN
    SELECT c.table_name, c.column_name
      FROM information_schema.columns c
      JOIN information_schema.tables t USING (table_schema, table_name)
     WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'
       AND c.column_name ~ '(^|_)node(_|$)'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME COLUMN %I TO %I', r.table_name, r.column_name,
      regexp_replace(r.column_name, '(^|_)node(_|$)', '\1outpost\2', 'g'));
  END LOOP;

  -- Constraints, then the indexes left (a constraint's backing index renames
  -- with the constraint).
  FOR r IN
    SELECT con.conname, cls.relname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND con.conname ~ '(^|_)node(_|$)'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I', r.relname, r.conname,
      regexp_replace(r.conname, '(^|_)node(_|$)', '\1outpost\2', 'g'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = 'public' AND c.relkind = 'i' AND c.relname ~ '(^|_)node(_|$)'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      regexp_replace(r.relname, '(^|_)node(_|$)', '\1outpost\2', 'g'));
  END LOOP;

  -- The checks that name the 'node' kind come off, the rows move (on every
  -- table carrying an owner_kind), and the checks go back on renamed.
  FOR r IN
    SELECT cls.relname, con.conname, pg_get_constraintdef(con.oid) AS def
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND con.contype = 'c'
       AND pg_get_constraintdef(con.oid) LIKE '%''node''::text%'
  LOOP
    EXECUTE format('ALTER TABLE public.%I DROP CONSTRAINT %I', r.relname, r.conname);
    defs := defs || format('ALTER TABLE public.%I ADD CONSTRAINT %I %s', r.relname, r.conname,
      replace(r.def, '''node''::text', '''outpost''::text'));
  END LOOP;

  FOR r IN
    SELECT c.table_name
      FROM information_schema.columns c
      JOIN information_schema.tables t USING (table_schema, table_name)
     WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE' AND c.column_name = 'owner_kind'
  LOOP
    EXECUTE format('UPDATE public.%I SET owner_kind = %L WHERE owner_kind = %L', r.table_name, 'outpost', 'node');
  END LOOP;
  UPDATE public.principal SET kind = 'outpost' WHERE kind = 'node';

  FOR i IN 1 .. coalesce(array_length(defs, 1), 0) LOOP
    EXECUTE defs[i];
  END LOOP;

  -- The partial unique index keyed on the kind.
  DROP INDEX public.tag_binding_outpost_key;
  CREATE UNIQUE INDEX tag_binding_outpost_key ON public.tag_binding USING btree (tag_id, outpost_id)
    WHERE (owner_kind = 'outpost'::text);

  -- Role permission nouns: node:<actions> becomes outpost:<actions>, entry
  -- order preserved. Seeded roles re-upsert from roles.yaml at boot; this
  -- carries operator-authored roles.
  UPDATE public.role
    SET permissions = (
      SELECT array_agg(
               CASE WHEN p LIKE 'node:%'
                    THEN 'outpost:' || substring(p FROM length('node:') + 1)
                    ELSE p END
               ORDER BY ord)
        FROM unnest(permissions) WITH ORDINALITY AS u(p, ord))
    WHERE EXISTS (SELECT 1 FROM unnest(permissions) AS p WHERE p LIKE 'node:%');

  -- Seeded rows keyed by name rename in place, keeping their ids, so every
  -- platform component, product and raised event keeps its reference.
  UPDATE public.component_type SET name = 'omniglass-outpost' WHERE name = 'omniglass-node';
  UPDATE public.product SET name = 'omniglass-outpost' WHERE name = 'omniglass-node';
  UPDATE public.event_rule SET name = 'outpost-silent' WHERE name = 'node-silent';
END $$;

-- migrate:down
DO $$
DECLARE
  r record;
  defs text[] := '{}';
  i int;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'outpost') THEN
    RETURN;
  END IF;

  UPDATE public.event_rule SET name = 'node-silent' WHERE name = 'outpost-silent';
  UPDATE public.product SET name = 'omniglass-node' WHERE name = 'omniglass-outpost';
  UPDATE public.component_type SET name = 'omniglass-node' WHERE name = 'omniglass-outpost';
  UPDATE public.role
    SET permissions = (
      SELECT array_agg(
               CASE WHEN p LIKE 'outpost:%'
                    THEN 'node:' || substring(p FROM length('outpost:') + 1)
                    ELSE p END
               ORDER BY ord)
        FROM unnest(permissions) WITH ORDINALITY AS u(p, ord))
    WHERE EXISTS (SELECT 1 FROM unnest(permissions) AS p WHERE p LIKE 'outpost:%');

  DROP INDEX public.tag_binding_outpost_key;

  ALTER INDEX public.endpoint_outpost_id_idx RENAME TO endpoint_node_name_idx;
  ALTER TABLE public.endpoint RENAME CONSTRAINT endpoint_outpost_id_fkey TO endpoint_node_name_fkey;
  ALTER TABLE public.endpoint RENAME COLUMN outpost_id TO node_name;

  FOR r IN
    SELECT cls.relname, con.conname, pg_get_constraintdef(con.oid) AS def
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND con.contype = 'c'
       AND pg_get_constraintdef(con.oid) LIKE '%''outpost''::text%'
  LOOP
    EXECUTE format('ALTER TABLE public.%I DROP CONSTRAINT %I', r.relname, r.conname);
    defs := defs || format('ALTER TABLE public.%I ADD CONSTRAINT %I %s', r.relname, r.conname,
      replace(r.def, '''outpost''::text', '''node''::text'));
  END LOOP;

  FOR r IN
    SELECT c.table_name
      FROM information_schema.columns c
      JOIN information_schema.tables t USING (table_schema, table_name)
     WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE' AND c.column_name = 'owner_kind'
  LOOP
    EXECUTE format('UPDATE public.%I SET owner_kind = %L WHERE owner_kind = %L', r.table_name, 'node', 'outpost');
  END LOOP;
  UPDATE public.principal SET kind = 'node' WHERE kind = 'outpost';

  FOR i IN 1 .. coalesce(array_length(defs, 1), 0) LOOP
    EXECUTE defs[i];
  END LOOP;

  CREATE UNIQUE INDEX tag_binding_outpost_key ON public.tag_binding USING btree (tag_id, outpost_id)
    WHERE (owner_kind = 'node'::text);

  FOR r IN
    SELECT con.conname, cls.relname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND con.conname ~ '(^|_)outpost(_|$)'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I', r.relname, r.conname,
      regexp_replace(r.conname, '(^|_)outpost(_|$)', '\1node\2', 'g'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = 'public' AND c.relkind = 'i' AND c.relname ~ '(^|_)outpost(_|$)'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      regexp_replace(r.relname, '(^|_)outpost(_|$)', '\1node\2', 'g'));
  END LOOP;
  FOR r IN
    SELECT c.table_name, c.column_name
      FROM information_schema.columns c
      JOIN information_schema.tables t USING (table_schema, table_name)
     WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'
       AND c.column_name ~ '(^|_)outpost(_|$)'
  LOOP
    EXECUTE format('ALTER TABLE public.%I RENAME COLUMN %I TO %I', r.table_name, r.column_name,
      regexp_replace(r.column_name, '(^|_)outpost(_|$)', '\1node\2', 'g'));
  END LOOP;

  ALTER SEQUENCE public.outpost_log_id_seq RENAME TO node_log_id_seq;
  ALTER TABLE public.outpost_log RENAME TO node_log;
  ALTER TABLE public.outpost RENAME TO node;
END $$;
