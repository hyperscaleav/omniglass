-- migrate:up

-- A principal can be soft-disabled: authentication refuses a disabled principal,
-- but its rows (and the audit trail that references it) are kept. Hard delete is
-- deliberately not offered, since audit_log.actor_principal_id references the
-- principal, so an actor that has ever acted cannot be removed.
alter table principal add column if not exists active boolean not null default true;

-- migrate:down
alter table principal drop column if exists active;
