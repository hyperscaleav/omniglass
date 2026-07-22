-- setting_override.scope names the settings cascade level a row binds at, and it
-- was the one tier column with nothing behind it. That gap is how the rename of the
-- least-specific tier from 'global' to 'platform' passed a full green suite while
-- the Go layer still read and wrote the retired name: every operator override was
-- orphaned in silence, with no error anywhere. A CHECK makes that class of drift
-- loud, at the only layer that sees every writer.
--
-- The legal set is the cascade levels that are persisted as rows, and today that is
-- 'platform' alone. The two broader levels are recomputed in memory on every
-- resolve and can never legally appear here: 'default' is reflected off the Settings
-- struct (settings.Defaults) and 'file' is the operator file captured at boot
-- (settings.NewService), so listing either would permit a row the resolver would
-- never read. The group and user rungs arrive with their own migration widening this
-- CHECK, which is the point: a tier has to be declared here to exist.
--
-- Idempotent: the constraint is dropped by name before it is added (Postgres cannot
-- alter a CHECK in place), so a re-run is a no-op. Pure DDL, no data component: the
-- rename migration already moved every row off the old value.

-- migrate:up

alter table setting_override drop constraint if exists setting_override_scope_check;
alter table setting_override add constraint setting_override_scope_check
  check (scope in ('platform'));

-- migrate:down

alter table setting_override drop constraint if exists setting_override_scope_check;
