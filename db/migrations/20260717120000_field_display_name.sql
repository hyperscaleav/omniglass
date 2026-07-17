-- migrate:up

-- A field's optional human label. The raw `name` stays the unique key and the
-- interpolation handle; display_name is presentation only, nullable, and falls
-- back to the name in the UI when unset. Additive and idempotent.
alter table field_definition add column if not exists display_name text;

-- migrate:down

alter table field_definition drop column if exists display_name;
