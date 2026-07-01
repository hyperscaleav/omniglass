-- migrate:up

-- Add the password credential kind: a local password (argon2id, PHC-encoded in
-- secret_hash), the auth method humans use, one per principal keyed by
-- principal_id. DDL is idempotent.
alter table credential drop constraint if exists credential_kind_check;
alter table credential add constraint credential_kind_check check (kind in ('bearer', 'password'));

-- At most one password credential per principal.
create unique index if not exists credential_one_password
    on credential (principal_id) where kind = 'password';

-- migrate:down

drop index if exists credential_one_password;
alter table credential drop constraint if exists credential_kind_check;
alter table credential add constraint credential_kind_check check (kind in ('bearer'));
