-- migrate:up

-- The floor: after the backfill, every product has a component_type and
-- every component has a product, so both classifications become mandatory,
-- and kind narrows to the three classes that survive vm's retirement
-- (backfilled to app in 20260807113000_product_type_backfill.sql).

alter table product alter column component_type_id set not null;
alter table component alter column product_id set not null;

alter table product drop constraint if exists product_kind_check;
alter table product add constraint product_kind_check check (kind in ('device', 'app', 'service'));

-- migrate:down

alter table product drop constraint if exists product_kind_check;
alter table product add constraint product_kind_check check (kind in ('device', 'app', 'service', 'vm'));

alter table component alter column product_id drop not null;
alter table product alter column component_type_id drop not null;
