-- migrate:up

-- Bearer credentials (session cookies and API tokens) gain an optional expiry
-- (issue #157). A session installed at login expires after a fixed lifetime, so a
-- stolen cookie is not valid forever; AuthenticateBearer treats a credential whose
-- expires_at has passed as absent. API tokens (the CLI token / bootstrap lanes) leave
-- it null and do not expire. Additive and idempotent.
alter table credential add column if not exists expires_at timestamptz;

-- migrate:down

alter table credential drop column if exists expires_at;
