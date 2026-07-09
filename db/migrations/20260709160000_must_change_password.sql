-- migrate:up
-- Force a password change on next login: an admin reset sets this flag, and the
-- user's own change-password clears it, so an admin-known secret is short-lived.
alter table human add column if not exists must_change_password boolean not null default false;

-- migrate:down
alter table human drop column if exists must_change_password;
