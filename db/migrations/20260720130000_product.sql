-- migrate:up

-- product ties the classification catalogs together: a concrete SKU from a vendor,
-- driven by a driver, of a kind (device/app/service/vm), providing capabilities. A
-- sub-product (parent_product_id) is a variant/SKU that inherits from its parent.
create table if not exists product (
    id                text primary key,
    display_name      text not null,
    vendor_id         text references vendor (id) on delete set null,
    driver_id         text references driver (id) on delete set null,
    kind              text not null default 'device',
    parent_product_id text references product (id) on delete set null,
    official          boolean not null default false,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now(),
    constraint product_kind_check check (kind in ('device', 'app', 'service', 'vm'))
);
create index if not exists product_vendor_idx on product (vendor_id);
create index if not exists product_driver_idx on product (driver_id);
create index if not exists product_parent_idx on product (parent_product_id);

-- product_capability: the capabilities a product provides (a video bar -> mic,
-- speaker, camera, codec). Both legs cascade; the pair is unique.
create table if not exists product_capability (
    id            uuid primary key default uuidv7(),
    product_id    text not null references product (id) on delete cascade,
    capability_id text not null references capability (id) on delete cascade,
    created_at    timestamptz not null default now(),
    unique (product_id, capability_id)
);
create index if not exists product_capability_product_idx on product_capability (product_id);
create index if not exists product_capability_capability_idx on product_capability (capability_id);

-- a component is (optionally) a product. on delete restrict: a product in use
-- cannot be deleted out from under its components.
alter table component add column if not exists product_id text
    references product (id) on delete restrict;
create index if not exists component_product_idx on component (product_id);

-- migrate:down

drop index if exists component_product_idx;
alter table component drop column if exists product_id;
drop table if exists product_capability;
drop table if exists product;
