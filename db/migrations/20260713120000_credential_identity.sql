-- migrate:up

-- Bearer credentials carry identifying metadata so a user can tell what a credential
-- is and where it came from (issues #193, #205). description names a token (required
-- when a token is minted, null for an auto-created session); user_agent and client_ip
-- record the device and address that created it. last_used_at already exists and is now
-- bumped on authentication. All additive and nullable; sessions leave description null.
alter table credential add column if not exists description text;
alter table credential add column if not exists user_agent text;
alter table credential add column if not exists client_ip text;

-- migrate:down

alter table credential drop column if exists client_ip;
alter table credential drop column if exists user_agent;
alter table credential drop column if exists description;
