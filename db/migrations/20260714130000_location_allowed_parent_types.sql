-- migrate:up

-- allowed_parent_types is the placement constraint: a set of location_type ids
-- and/or the reserved "root" sentinel (a placement at the top, no parent) a
-- location of this type may sit under. Empty (the default) is unconstrained,
-- so every existing custom type is unaffected until an operator populates it.
-- CreateLocation and the location move path enforce it forward-only; existing
-- placements are grandfathered. Idempotent.
alter table location_type add column if not exists allowed_parent_types text[] not null default '{}';

-- migrate:down

alter table location_type drop column if exists allowed_parent_types;
