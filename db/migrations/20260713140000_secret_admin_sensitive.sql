-- migrate:up

-- admin_sensitivity: a per-secret flag that flips a secret's actions to the
-- :admin tier, so a platform credential (a Zoom or Microsoft client secret)
-- stays admin/owner-only even when it sits at the same scope as an operational
-- one (a room's SNMP community). Placement scope gives locality; this flag gives
-- the same-scope sensitivity split. Default true is the safe floor: a secret is
-- admin-only until someone marks it operational. secret_type carries the default
-- the create form seeds. Additive and idempotent.
alter table secret add column if not exists admin_sensitive boolean not null default true;
alter table secret_type add column if not exists default_admin_sensitive boolean not null default true;

-- migrate:down

alter table secret drop column if exists admin_sensitive;
alter table secret_type drop column if exists default_admin_sensitive;
