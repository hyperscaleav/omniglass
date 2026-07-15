-- migrate:up

-- Every bearer credential is now time-bounded (reverses the tokens-never-expire
-- choice, issue #172): a web-login session and a CLI/API token both carry an
-- expires_at, so whether one is set no longer tells them apart. A purpose column
-- names the concept: 'session' for a web login, 'token' for a CLI/API credential.
-- Additive and idempotent.
alter table credential add column if not exists purpose text;

-- Backfill existing bearers so nothing is mislabeled: a bearer with an expiry was a
-- web-login session (the only bounded kind before this change), one without was a
-- CLI/API token. Password credentials keep a null purpose (the column is bearer-only).
update credential set purpose = case when expires_at is not null then 'session' else 'token' end
	where kind = 'bearer' and purpose is null;

-- migrate:down

alter table credential drop column if exists purpose;
