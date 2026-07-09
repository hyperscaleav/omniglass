-- migrate:up
-- Per-username brute-force protection: count consecutive failed logins and, past a
-- threshold, lock the account for a cooldown window. Both columns live on human
-- because login is by username; a service principal has no password lane to throttle.
alter table human add column if not exists failed_login_count int not null default 0;
alter table human add column if not exists locked_until timestamptz;

-- migrate:down
alter table human drop column if exists locked_until;
alter table human drop column if exists failed_login_count;
