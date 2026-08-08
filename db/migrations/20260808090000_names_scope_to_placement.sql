-- migrate:up

-- #627: a location/system/component name is unique within its placement, not
-- across the whole estate. Each entity keeps its old constraint's ROLE (a hot
-- exact-match lookup and a race-free insert guard) but the tree splits into the
-- buckets an operator actually reasons in: under a parent, at a location (for the
-- two tree types that carry one directly), or unplaced/root. Dropping the old
-- global UNIQUE constraint removes the only index on the bare name column, so
-- each table also gets a plain btree back for `where name = $1` (the ambiguity
-- scan in scopedByName, and the *NameTaken advisories) to run on.
--
-- system needs THREE buckets, not two, unlike location: system carries its own
-- parent_id (nested systems are real, systems_scope_test.go builds av / av-sub /
-- av-leaf) and CreateSystem never inherits a parent's location_id, so an
-- orphan-only split would let "edge" under "av" and "edge" under "lab" collide
-- in the same bucket. component has the same shape as system for the same
-- reason (nested components are real too).
--
-- node keeps its global node_name_key: a node's name is also its NATS subject,
-- a wire identity outside this tree's placement semantics.

alter table location drop constraint if exists location_name_key;
create unique index if not exists location_parent_name_key on location(parent_id, name) where parent_id is not null;
create unique index if not exists location_root_name_key   on location(name)            where parent_id is null;
create index if not exists location_name_idx on location(name);

alter table component drop constraint if exists component_name_key;
create unique index if not exists component_parent_name_key   on component(parent_id, name)   where parent_id is not null;
create unique index if not exists component_location_name_key on component(location_id, name) where parent_id is null and location_id is not null;
create unique index if not exists component_orphan_name_key   on component(name)              where parent_id is null and location_id is null;
create index if not exists component_name_idx on component(name);

alter table system drop constraint if exists system_name_key;
create unique index if not exists system_parent_name_key   on system(parent_id, name)   where parent_id is not null;
create unique index if not exists system_location_name_key on system(location_id, name) where parent_id is null and location_id is not null;
create unique index if not exists system_orphan_name_key   on system(name)              where parent_id is null and location_id is null;
create index if not exists system_name_idx on system(name);

-- name_generated marks a component whose name the platform picked (Task 13's
-- stem+ordinal generator) rather than an operator typing one. DEFAULT false,
-- not the epic's DEFAULT true, is deliberate: every row that exists before this
-- migration runs was operator-typed, so DEFAULT true would hand the pen to the
-- platform retroactively and let the first :move silently rename real estate.
-- The gateway writes the flag explicitly on insert from Task 13 onward; this
-- default only ever describes a pre-existing row.
alter table component add column if not exists name_generated boolean not null default false;

-- migrate:down
-- irreversible: a scoped-name estate cannot be re-globalised without renaming
-- rows first (two rows sharing a name today would violate the old global
-- constraint the moment it came back).
