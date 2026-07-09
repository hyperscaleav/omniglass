-- migrate:up

-- One-time backfill: populate the denormalized actor labels on audit rows that
-- predate the columns, so an old row still names its actor after that principal is
-- purged. New rows are written with the label already set (see writeAuditRes).
update audit_log set actor_username = principal_label(actor_principal_id)
    where actor_username is null and actor_principal_id is not null;
update audit_log set real_actor_username = principal_label(real_actor_principal_id)
    where real_actor_username is null and real_actor_principal_id is not null;

-- migrate:down

-- Data-only backfill; the columns are dropped by the schema migration's down.
select 1;
