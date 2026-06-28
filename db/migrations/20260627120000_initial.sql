-- migrate:up

-- The platform settings store: a small, generic key/value table for
-- ship-with-the-binary platform configuration. It is the one real table the
-- walking skeleton needs so `migrate` has something concrete to create and
-- `healthz` has a real schema to probe. Domain tables (identity, entities,
-- telemetry) arrive in later slices, each as its own additive migration.
--
-- DDL is idempotent (IF NOT EXISTS) so the migration is safe to apply against
-- an instance that may already carry partial state.
create table if not exists platform_setting (
    key        text        primary key,
    value      jsonb       not null default '{}'::jsonb,
    updated_at timestamptz not null default now()
);

-- migrate:down

drop table if exists platform_setting;
