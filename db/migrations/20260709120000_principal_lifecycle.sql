-- migrate:up

-- The principal lifecycle (issue #143): beyond disable (the reversible `active`
-- flag), a principal can be DEACTIVATED (a soft delete: hidden from the working
-- set, cannot authenticate, reversible) and then PURGED (a hard delete). This
-- column marks the soft delete; a null means the account is live.
alter table principal add column if not exists deactivated_at timestamptz;

-- Purge is a hard delete of the principal row. Its owned rows (human/service,
-- credential, grants, group memberships, impersonation sessions) already cascade,
-- but the audit trail must survive: an audit row records who did each action, and
-- purging that actor must not orphan or erase the history. So denormalize the
-- actor's human-readable label into each audit row, and flip the audit foreign
-- keys to ON DELETE SET NULL, so a purge keeps the "who" as text after the
-- principal is gone.
create or replace function principal_label(pid uuid) returns text
    language sql stable as $$
    select coalesce(
        (select username from human where principal_id = pid),
        (select label from service where principal_id = pid)
    );
$$;

alter table audit_log add column if not exists actor_username text;
alter table audit_log add column if not exists real_actor_username text;

alter table audit_log drop constraint if exists audit_log_actor_principal_id_fkey;
alter table audit_log add constraint audit_log_actor_principal_id_fkey
    foreign key (actor_principal_id) references principal (id) on delete set null;
alter table audit_log drop constraint if exists audit_log_real_actor_principal_id_fkey;
alter table audit_log add constraint audit_log_real_actor_principal_id_fkey
    foreign key (real_actor_principal_id) references principal (id) on delete set null;

-- migrate:down

alter table audit_log drop constraint if exists audit_log_actor_principal_id_fkey;
alter table audit_log add constraint audit_log_actor_principal_id_fkey
    foreign key (actor_principal_id) references principal (id);
alter table audit_log drop constraint if exists audit_log_real_actor_principal_id_fkey;
alter table audit_log add constraint audit_log_real_actor_principal_id_fkey
    foreign key (real_actor_principal_id) references principal (id);
alter table audit_log drop column if exists real_actor_username;
alter table audit_log drop column if exists actor_username;
drop function if exists principal_label(uuid);
alter table principal drop column if exists deactivated_at;
