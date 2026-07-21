-- migrate:up

-- The fields feature folds into the estate model: a declared field is a
-- property_value with declared provenance, and the per-type field catalog becomes
-- the per-product product_property contract (both added in the previous migration).
-- field_value and field_definition retire here; field_value first, since it
-- references the definition.
drop table if exists field_value;
drop table if exists field_definition;

-- component_type retires with them. It was the component's genus and the anchor the
-- field catalog hung off; the component's shape now comes from its product
-- (component.product_id, nullable: a productless component simply has no contract),
-- and the category it used to carry (display, codec) is expressed by the
-- capabilities that product provides. Drop the FK column before the table it
-- references.
alter table component drop column if exists component_type;
drop table if exists component_type;

-- migrate:down

-- Best-effort restore of the retired shape. This is a pre-release retirement with no
-- operator data behind it, so the down path recreates the empty structures rather
-- than reconstructing rows (nothing was migrated out of them).
create table if not exists component_type (
    id           text        primary key,
    official     boolean     not null default false,
    display_name text        not null,
    rank         integer,
    created_at   timestamptz not null default now()
);
alter table component add column if not exists component_type text references component_type (id) on delete cascade;

create table if not exists field_definition (
    id             uuid        primary key default uuidv7(),
    component_type text        not null references component_type (id) on delete cascade,
    name           text        not null,
    display_name   text,
    data_type      text        not null,
    required       boolean     not null default false,
    default_value  jsonb,
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now(),
    unique (component_type, name)
);

create table if not exists field_value (
    id           uuid        primary key default uuidv7(),
    field_id     uuid        not null references field_definition (id) on delete cascade,
    component_id uuid        not null references component (id) on delete cascade,
    value        jsonb       not null,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    unique (field_id, component_id)
);
