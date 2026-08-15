-- migrate:up

-- #613, reversing this slice's own floor before it merged: `label` is
-- NULLABLE, and unset is SQL NULL and nothing else (ADR-0118, rewritten).
--
-- The floor two migrations back made the column `NOT NULL DEFAULT ''`. The
-- evidence against it is in the diff that introduced it: every one of the seven
-- registry orderings had to be spelled `order by nullif(label, '') nulls last`,
-- which is the READ path undoing the storage choice. A representation every
-- reader has to convert back is the wrong representation. Postgres already has
-- a value for "there is nothing here", the orderings already sort it last by
-- default, and the only reason not to use it was a `CHECK` that a rule living
-- in Go at the gateway never needed.
--
-- Two things make NULL the ONLY spelling rather than a second one beside '':
--
--   1. This migration, which drops the defaults and converts every existing
--      empty (and whitespace-only) label to NULL, so no row carries the old
--      spelling.
--   2. `labelOrNull` / `labelPatch` in internal/storage/label_unset.go, the one
--      place the platform decides what an unset label looks like. Every write
--      path binds through them, and a behavioural guard drives every one of
--      those paths with an empty label and asserts the raw column is NULL.
--
-- Without (2) this trades one two-state mess for another, which is exactly what
-- the floor did. The migration is the smaller half.
--
-- The PEN is untouched: `label_generated` stays `NOT NULL DEFAULT false`. It
-- answers a different question ("who owns this field") and has a genuine third
-- state for nobody. `stem` and `abbrev` are untouched for the same reason they
-- always were: their NULL means inherit-from-an-ancestor.
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
    execute format('alter table %I alter column label drop default', t);
    execute format('alter table %I alter column label drop not null', t);
    -- btrim, not `= ''`: a whitespace-only label renders as a blank row and is
    -- unset by every surface that reads one, so it is unset here too.
    execute format('update %I set label = null where btrim(label) = ''''', t);
  end loop;
end $$;

-- The two named NOT NULL constraints go with the nullability they named. They
-- were renamed alongside the column two migrations back, so they are dropped
-- here under their current names.
alter table standard drop constraint if exists standard_label_not_null;
alter table vendor   drop constraint if exists vendor_label_not_null;

-- registry_shadow.image is an operator's forked copy of a registry row, stored
-- as jsonb keyed by column name. A cleared label has to survive as a PRESENT
-- key holding JSON null, not as a dropped key: `applyComponentTypeImage`
-- distinguishes the two, and a dropped key re-inherits the official row's
-- label, which is the opposite of what an operator who cleared it asked for.
update registry_shadow
   set image = jsonb_set(image, '{label}', 'null'::jsonb)
 where image ? 'label'
   and image ->> 'label' is not null
   and btrim(image ->> 'label') = '';

-- migrate:down

update registry_shadow
   set image = jsonb_set(image, '{label}', '""'::jsonb)
 where image ? 'label' and image ->> 'label' is null;

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
    execute format('alter table %I alter column label set not null', t);
    execute format('alter table %I alter column label set default ''''', t);
  end loop;
end $$;
