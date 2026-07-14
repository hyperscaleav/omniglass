-- migrate:up

-- rank was sort-only from the start (the location_type seed comment already said
-- so: "rank does NOT constrain nesting"). It never enforced anything, and the
-- upcoming allowed_parent_types placement constraint on location_type needs a
-- clean field to introduce without a stale, unused ordering column beside it.
-- Drop it from all three type registries; the registries now list alphabetically
-- by display_name (see internal/storage). Idempotent.
alter table location_type drop column if exists rank;
alter table system_type drop column if exists rank;
alter table component_type drop column if exists rank;

-- migrate:down

alter table location_type add column if not exists rank integer not null default 0;
alter table system_type add column if not exists rank integer not null default 0;
alter table component_type add column if not exists rank integer not null default 0;
