-- migrate:up

-- The value half of the field primitive: a literal a component sets for a field
-- defined on its type. field_value is the variable table narrowed to a single
-- owner (component only): no owner_kind arc, no cascade resolver. The effective
-- read coalesces this set value with the definition's default. DDL is idempotent.
create table if not exists field_value (
    id           uuid        primary key default uuidv7(),
    field_id     uuid        not null references field_definition (id) on delete cascade,
    component_id uuid        not null references component (id) on delete cascade,
    value        jsonb       not null,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    unique (field_id, component_id)
);

create index if not exists field_value_component_idx on field_value (component_id);
create index if not exists field_value_field_idx on field_value (field_id);

-- migrate:down

drop table if exists field_value;
