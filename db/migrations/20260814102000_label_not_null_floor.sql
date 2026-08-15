-- migrate:up

-- #613, the floor: `label` is NOT NULL DEFAULT '' on every table that carries
-- one, so "unset" has exactly one spelling in the schema (D3).
--
-- Before this the twenty-three labelled tables were split three ways: eleven
-- nullable, seven NOT NULL with no default, five already NOT NULL DEFAULT ''.
-- Three shapes for one fact meant a projection could not know whether to
-- coalesce, and an `order by label` sorted unset rows to opposite ends of the
-- list depending on which table it read.
--
-- Nullable is not kept as "the operator never set one", because the platform
-- can set one too: the PEN (label_generated, #657) is what distinguishes a
-- label an operator typed from one a rule rendered, on the three tables that
-- have rules. On the other twenty nothing but an operator writes the column, so
-- `label <> ''` is the whole answer and a third state buys nothing.
--
-- Both statements are idempotent: SET NOT NULL on an already-NOT-NULL column
-- and SET DEFAULT on a column that already has that default are both no-ops.
-- The backfill immediately before this one is what makes SET NOT NULL able to
-- succeed.
--
-- `stem` and `abbrev` are NOT in this sweep. Their NULL means "inherit from an
-- ancestor" (ResolveTypeFacts walks the parent chain until it finds a non-null),
-- a third state with real content, and collapsing it would turn every inheriting
-- type into one that overrides its parent with the empty string.
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
    execute format('alter table %I alter column label set not null', t);
    execute format('alter table %I alter column label set default ''''', t);
  end loop;
end $$;

-- migrate:down

-- Restores the three shapes exactly as they were, which is why the lists are
-- split rather than driven from one array: five tables shipped with
-- DEFAULT '' from their own migrations and must keep it.
do $$
declare
  t text;
  -- Nullable, no default, before this slice.
  was_nullable text[] := array[
    'command_type', 'component', 'event_type', 'human', 'location',
    'metric_type', 'node', 'principal_group', 'property_type', 'role', 'system'
  ];
  -- NOT NULL, no default, before this slice.
  was_bare_not_null text[] := array[
    'driver', 'location_type', 'product', 'secret_type', 'standard',
    'system_role', 'vendor'
  ];
begin
  foreach t in array was_nullable loop
    execute format('alter table %I alter column label drop default', t);
    execute format('alter table %I alter column label drop not null', t);
  end loop;
  foreach t in array was_bare_not_null loop
    execute format('alter table %I alter column label drop default', t);
  end loop;
end $$;
