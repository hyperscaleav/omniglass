-- migrate:up

-- #614 (task 2): a product is now classified by a component_type (the
-- taxonomy Task 1 introduced above product), and may declare its own icon
-- override, resolved beside component_type's inherited icon by the same
-- parent walk (ResolveTypeFacts). Both nullable here: the floor migration
-- (20260807116000_product_type_floor.sql) narrows component_type_id to NOT
-- NULL once the backfill (20260807113000) has filled every existing row. ON
-- DELETE RESTRICT on component_type_id, the same rule component_type's own
-- parent_id carries: a registry delete must never cascade through classified
-- rows.
--
-- kind's default drops in the same breath: the floor migration narrows
-- product_kind_check to device/app/service (vm retires, backfilled to app),
-- and kind is becoming a required field at the API. A silent default would
-- let a bare insert land on 'device' instead of failing loud once kind is no
-- longer optional in the contract.

alter table product add column if not exists component_type_id uuid references component_type(id) on delete restrict;
alter table product add column if not exists icon text;
alter table product alter column kind drop default;

-- migrate:down

alter table product alter column kind set default 'device';
alter table product drop column if exists icon;
alter table product drop column if exists component_type_id;
