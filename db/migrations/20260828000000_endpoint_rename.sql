-- migrate:up
-- The endpoint rename (#811, #603): the entity that names how a device API is
-- reached is an endpoint, not an interface, and its transport is a build-time
-- fact of the binary (ADR-0073), so the interface_type table retires in favor
-- of the Go registry (internal/transport). The rename carries everything that
-- spells the old noun: the table, the task arc's column, the role permission
-- strings, and the canonical reachability datapoint, whose row renames in
-- place so every uuid-keyed sample keeps its history. Every statement is
-- guarded so a partially-renamed database converges rather than errors.

-- The table itself.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables
             WHERE table_schema = 'public' AND table_name = 'interface')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables
             WHERE table_schema = 'public' AND table_name = 'endpoint') THEN
    ALTER TABLE public.interface RENAME TO endpoint;
  END IF;
END $$;

-- The transport name column, backfilled from the retiring type row while that
-- table still exists; the belt-and-braces second update covers a row whose
-- type row vanished (the name IS the transport, by the derived-name rule).
ALTER TABLE public.endpoint ADD COLUMN IF NOT EXISTS transport text;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables
             WHERE table_schema = 'public' AND table_name = 'interface_type') THEN
    UPDATE public.endpoint e SET transport = t.name
      FROM public.interface_type t
      WHERE e.type = t.id AND e.transport IS NULL;
  END IF;
END $$;
UPDATE public.endpoint SET transport = name WHERE transport IS NULL;
ALTER TABLE public.endpoint ALTER COLUMN transport SET NOT NULL;

-- The FK and its table retire.
ALTER TABLE public.endpoint DROP COLUMN IF EXISTS type;
DROP TABLE IF EXISTS public.interface_type;

-- The constraints and indexes code names, renamed with their table.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'interface_pkey') THEN
    ALTER TABLE public.endpoint RENAME CONSTRAINT interface_pkey TO endpoint_pkey;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'interface_component_name_key') THEN
    ALTER TABLE public.endpoint RENAME CONSTRAINT interface_component_name_key TO endpoint_component_name_key;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'interface_component_fkey') THEN
    ALTER TABLE public.endpoint RENAME CONSTRAINT interface_component_fkey TO endpoint_component_fkey;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'interface_node_name_fkey') THEN
    ALTER TABLE public.endpoint RENAME CONSTRAINT interface_node_name_fkey TO endpoint_node_name_fkey;
  END IF;
END $$;
ALTER INDEX IF EXISTS interface_node_name_idx RENAME TO endpoint_node_name_idx;

-- The task arc.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'task' AND column_name = 'interface_id')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'task' AND column_name = 'endpoint_id') THEN
    ALTER TABLE public.task RENAME COLUMN interface_id TO endpoint_id;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'task_interface_id_fkey') THEN
    ALTER TABLE public.task RENAME CONSTRAINT task_interface_id_fkey TO task_endpoint_id_fkey;
  END IF;
END $$;
ALTER INDEX IF EXISTS task_interface_idx RENAME TO task_endpoint_idx;

-- Role permission nouns: interface:<actions> becomes endpoint:<actions>,
-- entry order preserved, other nouns untouched. Seeded roles are re-upserted
-- from roles.yaml at boot anyway; this carries operator-authored roles.
UPDATE public.role
  SET permissions = (
    SELECT array_agg(
             CASE WHEN p LIKE 'interface:%'
                  THEN 'endpoint:' || substring(p FROM length('interface:') + 1)
                  ELSE p END
             ORDER BY ord)
      FROM unnest(permissions) WITH ORDINALITY AS u(p, ord))
  WHERE EXISTS (SELECT 1 FROM unnest(permissions) AS p WHERE p LIKE 'interface:%');

-- The canonical datapoint renames in place: samples reference the row by id,
-- so the history rides along.
UPDATE public.property_type SET name = 'endpoint-reachable'
  WHERE name = 'interface-reachable'
    AND NOT EXISTS (SELECT 1 FROM public.property_type WHERE name = 'endpoint-reachable');

-- migrate:down
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables
             WHERE table_schema = 'public' AND table_name = 'endpoint')
     AND NOT EXISTS (SELECT 1 FROM information_schema.tables
             WHERE table_schema = 'public' AND table_name = 'interface') THEN
    ALTER TABLE public.endpoint RENAME TO interface;
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS public.interface_type (
    name text NOT NULL,
    official boolean DEFAULT false NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    built boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    id uuid DEFAULT uuidv7() NOT NULL,
    CONSTRAINT interface_type_pkey PRIMARY KEY (id),
    CONSTRAINT interface_type_name_key UNIQUE (name)
);
INSERT INTO public.interface_type (name, description)
  SELECT DISTINCT transport, '' FROM public.interface
  ON CONFLICT (name) DO NOTHING;

ALTER TABLE public.interface ADD COLUMN IF NOT EXISTS type uuid;
UPDATE public.interface i SET type = t.id
  FROM public.interface_type t WHERE t.name = i.transport AND i.type IS NULL;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'interface' AND column_name = 'type') THEN
    ALTER TABLE public.interface ALTER COLUMN type SET NOT NULL;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'interface_type_fkey') THEN
    ALTER TABLE public.interface
      ADD CONSTRAINT interface_type_fkey FOREIGN KEY (type) REFERENCES public.interface_type(id);
  END IF;
END $$;
ALTER TABLE public.interface DROP COLUMN IF EXISTS transport;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'endpoint_pkey') THEN
    ALTER TABLE public.interface RENAME CONSTRAINT endpoint_pkey TO interface_pkey;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'endpoint_component_name_key') THEN
    ALTER TABLE public.interface RENAME CONSTRAINT endpoint_component_name_key TO interface_component_name_key;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'endpoint_component_fkey') THEN
    ALTER TABLE public.interface RENAME CONSTRAINT endpoint_component_fkey TO interface_component_fkey;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'endpoint_node_name_fkey') THEN
    ALTER TABLE public.interface RENAME CONSTRAINT endpoint_node_name_fkey TO interface_node_name_fkey;
  END IF;
END $$;
ALTER INDEX IF EXISTS endpoint_node_name_idx RENAME TO interface_node_name_idx;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'task' AND column_name = 'endpoint_id')
     AND NOT EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_name = 'task' AND column_name = 'interface_id') THEN
    ALTER TABLE public.task RENAME COLUMN endpoint_id TO interface_id;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'task_endpoint_id_fkey') THEN
    ALTER TABLE public.task RENAME CONSTRAINT task_endpoint_id_fkey TO task_interface_id_fkey;
  END IF;
END $$;
ALTER INDEX IF EXISTS task_endpoint_idx RENAME TO task_interface_idx;

UPDATE public.role
  SET permissions = (
    SELECT array_agg(
             CASE WHEN p LIKE 'endpoint:%'
                  THEN 'interface:' || substring(p FROM length('endpoint:') + 1)
                  ELSE p END
             ORDER BY ord)
      FROM unnest(permissions) WITH ORDINALITY AS u(p, ord))
  WHERE EXISTS (SELECT 1 FROM unnest(permissions) AS p WHERE p LIKE 'endpoint:%');

UPDATE public.property_type SET name = 'interface-reachable'
  WHERE name = 'endpoint-reachable'
    AND NOT EXISTS (SELECT 1 FROM public.property_type WHERE name = 'interface-reachable');
