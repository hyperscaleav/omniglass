-- migrate:up

-- #686: the NAME's pen spreads from component to system and location, and the
-- ordinal beside it spreads to system.
--
-- name_generated is the same column and the same polarity component already
-- carries (20260808090000_names_scope_to_placement.sql): true means the
-- platform picked this name and a later act on the row (a move, a reclassify,
-- :resetName) may re-mint it; false means an operator typed it and nothing
-- rewrites it but that operator.
--
-- DEFAULT false, and NO BACKFILL BESIDE THIS FILE, deliberately. It is the
-- argument the component column's own migration already records: every row that
-- exists when this runs was named by an operator, so a default of true, or a
-- backfill to true, would hand the pen to the platform retroactively and let
-- the first :move silently rename real estate. There is no transformation to
-- run, so there is no one-time backfill migration next to this one; the default
-- IS the correct value for an existing row. That is the opposite of the LABEL
-- pen's backfill (20260810110000_label_pen_backfill.sql), and the difference is
-- the field: "no label" is a state the platform may safely claim, while a row
-- always has a name and somebody typed it.
--
-- ordinal lands on system ONLY. It means "the number the platform allocated for
-- this row's name" and is nullable for the reason the component column is
-- (20260810090000_component_ordinal.sql): a typed name and a renamed row have
-- no such number, and absent is how that gets written down rather than a
-- fabricated placeholder. A location gets the pen and not the column because a
-- location cannot generate a name yet: location_type carries no stem, so there
-- is no mint for an ordinal to be allocated by until #687 gives it a name rule,
-- and a column no writer can fill is a fact waiting to be read wrongly.
--
-- No unique index on the ordinal, matching ADR-0097: two rows colliding on
-- (bucket, stem, ordinal) already collide on the NAME, which the scoped-name
-- unique indexes refuse, and allocation is serialized ahead of them by the
-- transaction-scoped advisory lock in namegen.go.
--
-- The check is a floor on the value's domain, not on concurrency. Dropped first
-- so a re-run against a partially-migrated database is a no-op rather than a
-- duplicate-constraint error (Postgres has no ADD CONSTRAINT IF NOT EXISTS).

alter table system add column if not exists name_generated boolean not null default false;
alter table system add column if not exists ordinal integer;

alter table system drop constraint if exists system_ordinal_check;
alter table system add constraint system_ordinal_check check (ordinal is null or ordinal >= 1);

alter table location add column if not exists name_generated boolean not null default false;

-- migrate:down

alter table system drop constraint if exists system_ordinal_check;
alter table system drop column if exists ordinal;
alter table system drop column if exists name_generated;
alter table location drop column if exists name_generated;
