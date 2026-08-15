-- migrate:up

-- #613, the data half of "unset has one spelling": collapse NULL into ''.
--
-- Eleven of the twenty-three labelled tables were nullable, so "no label" was
-- spelled NULL there and '' on the twelve that were not. Two spellings for one
-- fact is what puts a coalesce in every projection and, worse, what makes an
-- `order by label` mean opposite things on neighbouring tables: Postgres sorts
-- NULL last and '' first. The floor migration that follows makes the column
-- NOT NULL DEFAULT '', and this is the backfill that lets it.
--
-- A backfill, not DDL, and therefore its own migration: transforming existing
-- operator data is the third bucket and never rides in a schema migration.
--
-- This runs BEFORE the floor and AFTER the rename, so it names the column
-- `label`.
do $$
declare
  t text;
  labelled text[] := array[
    'choice_alternate', 'command_type', 'component', 'component_type', 'driver',
    'event_type', 'human', 'location', 'location_type', 'metric_type', 'node',
    'principal_group', 'product', 'property_type', 'role', 'role_choice',
    'secret_type', 'standard', 'system', 'system_role', 'system_type', 'task',
    'vendor'
  ];
begin
  foreach t in array labelled loop
    execute format('update %I set label = '''' where label is null', t);
  end loop;
end $$;

-- registry_shadow.image is an operator's version of a shipped registry row,
-- stored as jsonb keyed by COLUMN NAME (component_type and location_type both
-- write a `display_name` key, and both list queries read it back with
-- `image->>'display_name'`). The column rename cannot reach inside a jsonb
-- value, so an operator who forked a registry row before this migration would
-- have their relabelling silently ignored the moment the gateway started
-- asking for `image->>'label'`. This is the one place in the schema where the
-- old word survives as DATA rather than as an identifier.
update registry_shadow
   set image = (image - 'display_name') || jsonb_build_object('label', image -> 'display_name')
 where image ? 'display_name';

-- migrate:down

-- The reverse is lossy on purpose in one direction only: '' and NULL were both
-- "unset" before, so restoring NULL for every empty label is exact for every
-- row that meant unset and wrong for nobody, because a row that meant "the
-- empty string" as a LABEL is a row that renders its name either way.
do $$
declare
  t text;
  nullable text[] := array[
    'command_type', 'component', 'event_type', 'human', 'location',
    'metric_type', 'node', 'principal_group', 'property_type', 'role', 'system'
  ];
begin
  foreach t in array nullable loop
    execute format('update %I set label = null where label = ''''', t);
  end loop;
end $$;

update registry_shadow
   set image = (image - 'label') || jsonb_build_object('display_name', image -> 'label')
 where image ? 'label';
