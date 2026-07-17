-- migrate:up

-- The field tier: a typed field declared on a component_type (the schema half of
-- the field primitive, mirroring variable's value_type but lifted onto a type).
-- Components of that type carry the value in field_value. DDL is
-- idempotent.
create table if not exists field_definition (
    id             uuid        primary key default uuidv7(),
    component_type text        not null references component_type (id) on delete cascade,
    name           text        not null,
    data_type      text        not null check (data_type in ('string', 'int', 'float', 'bool', 'json')),
    default_value  jsonb,
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now(),
    unique (component_type, name)
);

create index if not exists field_definition_component_type_idx on field_definition (component_type);

-- migrate:down

drop table if exists field_definition;
