-- migrate:up

-- A role gains an operator-facing display name and a description, so the console
-- can teach what each built-in role grants (the Roles view and the grant-builder
-- tooltips). Both are nullable: a role without them falls back to its id.
alter table role add column if not exists display_name text;
alter table role add column if not exists description text;

-- migrate:down
alter table role drop column if exists description;
alter table role drop column if exists display_name;
