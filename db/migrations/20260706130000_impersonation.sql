-- migrate:up

-- The real actor behind an impersonated request. When an admin acts as another
-- principal, the audit row records both the impersonated principal (actor_principal_id)
-- and the real admin (real_actor_principal_id), so accountability is never lost.
-- Null for a normal, non-impersonated action.
alter table audit_log add column if not exists real_actor_principal_id uuid references principal (id);

-- An impersonation session: a bearer-hash-addressed grant for an admin to view as
-- (read-only) or act as (full) a target principal, for a bounded, revocable time.
-- It is deliberately a separate table from credential: a credential is how a
-- principal authenticates as ITSELF, while this authenticates as someone else on
-- another's behalf, a materially different fact with its own expiry/revoke/list.
create table if not exists impersonation_session (
    id                      uuid        primary key default uuidv7(),
    token_hash              bytea       not null unique,
    target_principal_id     uuid        not null references principal (id) on delete cascade,
    real_actor_principal_id uuid        not null references principal (id) on delete cascade,
    mode                    text        not null check (mode in ('view_as', 'act_as')),
    created_at              timestamptz not null default now(),
    expires_at              timestamptz not null,
    revoked_at              timestamptz
);

-- The active-session sweep / listing index (who is impersonating whom right now);
-- the token_hash lookup on every request is served by the unique constraint.
create index if not exists impersonation_session_active_idx
    on impersonation_session (expires_at) where revoked_at is null;

-- migrate:down
drop table if exists impersonation_session;
alter table audit_log drop column if exists real_actor_principal_id;
