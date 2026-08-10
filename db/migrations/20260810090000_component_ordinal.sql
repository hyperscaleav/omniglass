-- migrate:up

-- #681: the ordinal a generated name was minted with becomes a stored fact
-- rather than something every reader picks back out of the name string.
--
-- NULLABLE is load-bearing, not laziness. The column means "the number the
-- platform allocated for this row's name", and there are rows for which that
-- number does not exist: a name the operator typed, and a name the operator
-- renamed (:rename hands over the name and nulls this in the same statement).
-- Absent is the honest way to write that down. The alternative, some
-- placeholder number, is exactly the defect #654 fixed in the bare render: a
-- fabricated ordinal ends up stamped on a physical label as a digit that
-- appears nowhere in the entity's name.
--
-- No unique index accompanies it, deliberately. A "one ordinal per (placement
-- bucket, stem)" index cannot even be expressed here, since the stem is
-- resolved from the component_type chain rather than stored, and it would buy
-- nothing if it could: for a platform-named row the name IS the stem and the
-- ordinal formatted together, so two rows colliding on both already collide on
-- the name, which the scoped-name unique indexes
-- (20260808090000_names_scope_to_placement.sql) refuse. Allocation is
-- serialized ahead of that by the transaction-scoped advisory lock in
-- namegen.go. A redundant constraint here would add a second 23505 to map and
-- a second way for a race to surface as a 500.
--
-- The check is a floor on the value's domain, not on concurrency: ordinals
-- start at 1 and count up, so 0 or a negative is a code defect rather than
-- anything an operator can reach. Dropped first so re-running this migration
-- against a partially-migrated database is a no-op rather than a duplicate
-- constraint error (Postgres has no ADD CONSTRAINT IF NOT EXISTS).

alter table component add column if not exists ordinal integer;

alter table component drop constraint if exists component_ordinal_check;
alter table component add constraint component_ordinal_check check (ordinal is null or ordinal >= 1);

-- migrate:down

alter table component drop constraint if exists component_ordinal_check;
alter table component drop column if exists ordinal;
