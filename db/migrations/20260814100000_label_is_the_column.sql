-- migrate:up

-- #613: the column an operator reads is called `label`.
--
-- The word already won everywhere except the column it names. internal/label/
-- is the rule engine (ADR-0098), `label_rule` is a column on five tables plus a
-- table of its own, the API says :renderLabel / :previewLabels /
-- :recomputeLabels, and forty console files say entityLabel / labelPen /
-- labelGenerated. Until this migration a route called :recomputeLabels wrote a
-- column called display_name. The rename finishes a word rather than
-- introducing one.
--
-- The PEN renames with it. display_name_generated (#657) is the fact that
-- answers "did an operator type this, or did the platform render it from a
-- rule". A renamed column beside an unrenamed pen is the inconsistency that
-- deferred the pen's naming to this issue in the first place, so both halves
-- move in one migration or neither does.
--
-- Postgres has no RENAME COLUMN IF EXISTS, so each rename is guarded on the
-- catalog: the old column present AND the new one absent. Re-running is a
-- no-op. The guard is written once and driven over a list rather than copied
-- twenty-three times, because twenty-three copies of an eight-line block is
-- twenty-three chances to typo a table name into a silent skip, and the list is
-- the part a reviewer actually needs to check.
--
-- What is NOT renamed, deliberately:
--
--   `label_rule` (on component_type, system_type, location_type, product,
--   standard, plus the label_rule table): already the right word, already
--   meaning the template that PRODUCES a label. After this migration `label`
--   sits beside `label_rule` on the same tables, which is coherent.
--
--   `stem` and `abbrev`: nullable, and their NULL means "inherit from an
--   ancestor", a third state with real content that the normalization below
--   would destroy.
--
--   audit_log.old / audit_log.new: those jsonb images record what a row looked
--   like at the time it was written, under the field names it had at the time.
--   Rewriting them would falsify the record.
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
  penned text[] := array['component', 'location', 'system'];
begin
  foreach t in array labelled loop
    if exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'display_name')
       and not exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'label') then
      execute format('alter table %I rename column display_name to label', t);
    end if;
  end loop;

  foreach t in array penned loop
    if exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'display_name_generated')
       and not exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'label_generated') then
      execute format('alter table %I rename column display_name_generated to label_generated', t);
    end if;
  end loop;
end $$;

-- Two tables spell their NOT NULL as a NAMED constraint (init.sql:707, :902).
-- Renaming a column does not rename a constraint over it, so the name would
-- keep saying display_name forever.
do $$ begin
  if exists (select 1 from pg_constraint where conname = 'standard_display_name_not_null')
     and not exists (select 1 from pg_constraint where conname = 'standard_label_not_null') then
    alter table standard rename constraint standard_display_name_not_null to standard_label_not_null;
  end if;
  if exists (select 1 from pg_constraint where conname = 'vendor_display_name_not_null')
     and not exists (select 1 from pg_constraint where conname = 'vendor_label_not_null') then
    alter table vendor rename constraint vendor_display_name_not_null to vendor_label_not_null;
  end if;
end $$;

-- migrate:down

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
  penned text[] := array['component', 'location', 'system'];
begin
  foreach t in array penned loop
    if exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'label_generated')
       and not exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'display_name_generated') then
      execute format('alter table %I rename column label_generated to display_name_generated', t);
    end if;
  end loop;

  foreach t in array labelled loop
    if exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'label')
       and not exists (select 1 from information_schema.columns
                where table_schema = 'public' and table_name = t and column_name = 'display_name') then
      execute format('alter table %I rename column label to display_name', t);
    end if;
  end loop;
end $$;

do $$ begin
  if exists (select 1 from pg_constraint where conname = 'standard_label_not_null')
     and not exists (select 1 from pg_constraint where conname = 'standard_display_name_not_null') then
    alter table standard rename constraint standard_label_not_null to standard_display_name_not_null;
  end if;
  if exists (select 1 from pg_constraint where conname = 'vendor_label_not_null')
     and not exists (select 1 from pg_constraint where conname = 'vendor_display_name_not_null') then
    alter table vendor rename constraint vendor_label_not_null to vendor_display_name_not_null;
  end if;
end $$;
