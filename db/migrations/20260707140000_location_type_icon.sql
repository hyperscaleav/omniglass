-- migrate:up

-- Give the location_type registry an icon: a small glyph key (kebab, e.g.
-- 'building', 'layers') the console resolves to an SVG and renders as the leading
-- glyph on every location of that type, so a campus reads differently from a
-- building at a glance in the tree. Stored as a plain key, not validated here:
-- the registry has no operator write surface yet, so only the boot-seed writes
-- it. The default 'map-pin' covers any pre-existing row until the seed upserts
-- the real per-type icon on next boot. Idempotent.
alter table location_type add column if not exists icon text not null default 'map-pin';

-- migrate:down

alter table location_type drop column if exists icon;
