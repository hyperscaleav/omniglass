-- migrate:up

-- One-time data backfill ahead of the product/component_type floor
-- (20260807116000_product_type_floor.sql, applied right after this file):
-- every product needs a component_type and every component needs a product
-- before either column can turn NOT NULL.
--
-- A backfill may create the rows it needs, so the three generic
-- component_types and their matching generic products are seeded here too,
-- ahead of the boot-seed phase (internal/seed/component_types.yaml,
-- products.yaml upsert the same rows authoritatively on every server start).
-- This insert is what makes the generics exist on a database that only ever
-- migrates (a test harness, or an operator running `migrate` standalone
-- without a subsequent `server` boot).

insert into component_type (name, display_name, stem, icon, abbrev, official)
values
    ('generic-device', 'Generic Device', 'device', 'box', 'dev', true),
    ('generic-app', 'Generic App', 'app', 'app-window', 'app', true),
    ('generic-service', 'Generic Service', 'service', 'server', 'svc', true)
on conflict (name) do nothing;

insert into product (name, display_name, kind, component_type_id, official)
select v.name, v.display_name, v.kind, ct.id, true
from (values
    ('generic-device', 'Generic Device', 'device'),
    ('generic-app', 'Generic App', 'app'),
    ('generic-service', 'Generic Service', 'service')
) as v(name, display_name, kind)
join component_type ct on ct.name = v.name
on conflict (name) do nothing;

-- vm retires as a product kind (the floor narrows product_kind_check to
-- device/app/service); every existing vm product becomes an app, the closest
-- of the three surviving classes.
update product set kind = 'app' where kind = 'vm';

-- Every existing product gets a component_type matching its kind, so the
-- floor's NOT NULL never has anything left to reject. Idempotent: the null
-- guard makes a re-run a no-op.
update product set component_type_id = (
    select id from component_type where name = case
        when product.kind = 'service' then 'generic-service'
        when product.kind = 'app' then 'generic-app'
        else 'generic-device'
    end
) where component_type_id is null;

-- Every existing component gets a product, defaulting to the generic device:
-- the floor makes product a required classification, so a component that
-- predates this release becomes an unclassified generic-device instance
-- rather than a row the NOT NULL would otherwise refuse outright. Idempotent:
-- the null guard makes a re-run a no-op.
update component set product_id = (select id from product where name = 'generic-device')
where product_id is null;

-- migrate:down

-- Not meaningfully reversible: which products were originally 'vm' before
-- this backfill ran, and which components genuinely had no product, is not
-- recoverable from the row shape alone. The floor migration's own down
-- (applied before this one, since it runs after this file going forward)
-- already restores nullability; this down leaves the backfilled data in
-- place rather than guess at undoing it.
