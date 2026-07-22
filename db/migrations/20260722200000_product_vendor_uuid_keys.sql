-- migrate:up

-- vendor and product take uuid primary keys, and their kebab id becomes `name`:
-- a unique, RENAMEABLE handle. This is the shape `tag` already has (id uuid,
-- name text unique) and the shape every estate entity has after ADR-0056.
--
-- The registries were the last place a foreign key pointed at a mutable,
-- human-authored string. A product id is one typo or rebrand away from being
-- wrong forever, and two device packs both defining `cisco-room-kit-pro` collide
-- on the primary key itself.
--
-- Five inbound foreign keys move: product.vendor_id, product.parent_product_id
-- (self-referential), product_capability.product_id, product_property.product_id,
-- and component.product_id.

-- Every inbound reference is dropped first: a primary key cannot be dropped while
-- a foreign key still depends on it.
alter table product          drop constraint if exists product_vendor_id_fkey;
alter table product          drop constraint if exists product_parent_product_id_fkey;
alter table product_capability drop constraint if exists product_capability_product_id_fkey;
alter table product_capability drop constraint if exists product_capability_product_id_capability_id_key;
alter table product_property drop constraint if exists product_property_product_id_fkey;
alter table product_property drop constraint if exists product_property_product_id_property_name_key;
alter table component        drop constraint if exists component_product_id_fkey;

-- vendor: the kebab id becomes `name`, a uuid becomes the key ------------------
alter table vendor rename column id to name;
alter table vendor add column if not exists uuid_id uuid not null default uuidv7();
alter table vendor drop constraint if exists component_make_pkey;
alter table vendor drop constraint if exists vendor_pkey;
alter table vendor add constraint vendor_pkey primary key (uuid_id);
alter table vendor add constraint vendor_name_key unique (name);

-- product: the same ------------------------------------------------------------
alter table product rename column id to name;
alter table product add column if not exists uuid_id uuid not null default uuidv7();
alter table product drop constraint if exists product_pkey;
alter table product add constraint product_pkey primary key (uuid_id);
alter table product add constraint product_name_key unique (name);

-- repoint every reference at the new keys, resolving through the handle --------
alter table product add column if not exists vendor_uuid uuid;
update product p set vendor_uuid = v.uuid_id from vendor v where v.name = p.vendor_id;
alter table product drop column vendor_id;
alter table product rename column vendor_uuid to vendor_id;

alter table product add column if not exists parent_uuid uuid;
update product p set parent_uuid = q.uuid_id from product q where q.name = p.parent_product_id;
alter table product drop column parent_product_id;
alter table product rename column parent_uuid to parent_product_id;

alter table product_capability add column if not exists product_uuid uuid;
update product_capability pc set product_uuid = p.uuid_id from product p where p.name = pc.product_id;
alter table product_capability drop column product_id;
alter table product_capability rename column product_uuid to product_id;

alter table product_property add column if not exists product_uuid uuid;
update product_property pp set product_uuid = p.uuid_id from product p where p.name = pp.product_id;
alter table product_property drop column product_id;
alter table product_property rename column product_uuid to product_id;

alter table component add column if not exists product_uuid uuid;
update component c set product_uuid = p.uuid_id from product p where p.name = c.product_id;
alter table component drop column product_id;
alter table component rename column product_uuid to product_id;

-- the surrogates are only now safe to name `id`, since nothing references the text
alter table vendor  rename column uuid_id to id;
alter table product rename column uuid_id to id;

alter table product add constraint product_vendor_id_fkey
    foreign key (vendor_id) references vendor (id) on delete set null;
alter table product add constraint product_parent_product_id_fkey
    foreign key (parent_product_id) references product (id) on delete set null;
alter table product_capability add constraint product_capability_product_id_fkey
    foreign key (product_id) references product (id) on delete cascade;
alter table product_capability add constraint product_capability_product_id_capability_id_key
    unique (product_id, capability_id);
alter table product_property add constraint product_property_product_id_fkey
    foreign key (product_id) references product (id) on delete cascade;
alter table product_property add constraint product_property_product_id_property_name_key
    unique (product_id, property_name);
alter table component add constraint component_product_id_fkey
    foreign key (product_id) references product (id) on delete restrict;

create index if not exists product_vendor_idx on product (vendor_id);
create index if not exists product_parent_idx on product (parent_product_id);
create index if not exists product_capability_product_idx on product_capability (product_id);
create index if not exists component_product_idx on component (product_id);
create index if not exists vendor_name_idx on vendor (name);
create index if not exists product_name_idx on product (name);

-- migrate:down

-- Every reference points at a row that still exists, so its current name is the
-- right answer and the reversal is exact.

alter table component drop constraint if exists component_product_id_fkey;
alter table component add column if not exists product_name text;
update component c set product_name = p.name from product p where p.id = c.product_id;
alter table component drop column product_id;
alter table component rename column product_name to product_id;

alter table product_property drop constraint if exists product_property_product_id_fkey;
alter table product_property add column if not exists product_name text;
update product_property pp set product_name = p.name from product p where p.id = pp.product_id;
alter table product_property drop column product_id;
alter table product_property rename column product_name to product_id;

alter table product_capability drop constraint if exists product_capability_product_id_fkey;
alter table product_capability drop constraint if exists product_capability_product_id_capability_id_key;
alter table product_capability add column if not exists product_name text;
update product_capability pc set product_name = p.name from product p where p.id = pc.product_id;
alter table product_capability drop column product_id;
alter table product_capability rename column product_name to product_id;

alter table product drop constraint if exists product_parent_product_id_fkey;
alter table product add column if not exists parent_name text;
update product p set parent_name = q.name from product q where q.id = p.parent_product_id;
alter table product drop column parent_product_id;
alter table product rename column parent_name to parent_product_id;

alter table product drop constraint if exists product_vendor_id_fkey;
alter table product add column if not exists vendor_name text;
update product p set vendor_name = v.name from vendor v where v.id = p.vendor_id;
alter table product drop column vendor_id;
alter table product rename column vendor_name to vendor_id;

alter table product drop constraint if exists product_pkey;
alter table product drop constraint if exists product_name_key;
alter table product drop column id;
alter table product rename column name to id;
alter table product add constraint product_pkey primary key (id);

alter table vendor drop constraint if exists vendor_pkey;
alter table vendor drop constraint if exists vendor_name_key;
alter table vendor drop column id;
alter table vendor rename column name to id;
alter table vendor add constraint vendor_pkey primary key (id);

alter table product add constraint product_vendor_id_fkey
    foreign key (vendor_id) references vendor (id) on delete set null;
alter table product add constraint product_parent_product_id_fkey
    foreign key (parent_product_id) references product (id) on delete set null;
alter table product_capability add constraint product_capability_product_id_fkey
    foreign key (product_id) references product (id) on delete cascade;
alter table product_capability add constraint product_capability_product_id_capability_id_key
    unique (product_id, capability_id);
alter table product_property add constraint product_property_product_id_fkey
    foreign key (product_id) references product (id) on delete cascade;
alter table product_property add constraint product_property_product_id_property_name_key
    unique (product_id, property_name);
alter table component add constraint component_product_id_fkey
    foreign key (product_id) references product (id) on delete restrict;
