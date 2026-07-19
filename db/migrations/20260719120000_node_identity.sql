-- Node identity (N1): a node gains a display name and a descriptive location.
-- display_name mirrors component/system/location (the label above the key).
-- location_name is a descriptive placement (which room the box sits in), not a
-- scope: a node stays estate-wide. ON DELETE SET NULL so a node survives its
-- location's deletion (its placement clears). name stays the immutable key.
-- migrate:up
alter table node add column if not exists display_name text;
alter table node add column if not exists location_name text references location (name) on delete set null;

-- migrate:down
alter table node drop column if exists location_name;
alter table node drop column if exists display_name;
