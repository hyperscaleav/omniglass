-- migrate:up

-- Whether a field must be set on every component of its type. A required field
-- has no "unset" state at the surface: the operator must supply a value (or the
-- definition's default stands in). Presentation and validation only; the raw
-- `name` stays the unique key. Additive and idempotent.
alter table field_definition add column if not exists required boolean not null default false;

-- migrate:down

alter table field_definition drop column if exists required;
