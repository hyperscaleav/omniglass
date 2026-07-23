-- migrate:up

-- Restore constraints and indexes silently dropped during the #262 / #343
-- column recreations. Each existed before those epics and was lost when its
-- column was rebuilt (Postgres drops the NOT NULL and every index a recreated
-- column carried). The sibling columns on the same tables kept theirs, so the
-- loss is an asymmetry, not an intended relaxation.

-- NOT NULL on the join and contract foreign keys. A product_property /
-- product_capability / standard_property / location_type_property row with a
-- NULL side is meaningless: the row exists only to tie the two ends together.
-- The partner column on each of these tables is already NOT NULL.
alter table product_property alter column product_id set not null;
alter table product_property alter column property_id set not null;
alter table product_capability alter column product_id set not null;
alter table standard_property alter column property_id set not null;
alter table location_type_property alter column property_id set not null;

-- Owner index on state_datapoint, dropped when its `key` column became
-- `property_id` (the index was defined on `key`). The sibling metric_datapoint
-- and event tables rebuilt theirs on property_id in the same migration; this one
-- was missed, leaving the LatestState / StateTransitions / health read path a
-- sequential scan.
create index if not exists state_datapoint_owner_idx
    on state_datapoint (component_id, property_id, instance, ts desc)
    where component_id is not null;

-- Capability-side index on product_capability, dropped when `capability_id` was
-- recreated during the capability uuid conversion. The product-side index was
-- rebuilt; this one was not, so deleting a capability (which RESTRICT-checks
-- product_capability) and listing products by capability both scan.
create index if not exists product_capability_capability_idx
    on product_capability (capability_id);

-- migrate:down
drop index if exists product_capability_capability_idx;
drop index if exists state_datapoint_owner_idx;
alter table location_type_property alter column property_id drop not null;
alter table standard_property alter column property_id drop not null;
alter table product_capability alter column product_id drop not null;
alter table product_property alter column property_id drop not null;
alter table product_property alter column product_id drop not null;
