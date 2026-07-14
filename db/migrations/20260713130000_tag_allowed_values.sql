-- migrate:up

-- A tag key can constrain its values to a declared enum: allowed_values is the
-- set a bound value must belong to. An empty set (the default) means the key is
-- free-text, so existing keys are unaffected. When non-empty, the binding write
-- enforces membership in the app. This answers the value-domain governance
-- question for the enum case; a typed value_type and input normalization stay
-- open. DDL is idempotent.
alter table tag add column if not exists allowed_values text[] not null default '{}';

-- migrate:down

alter table tag drop column if exists allowed_values;
