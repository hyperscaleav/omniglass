-- migrate:up
-- A human gains an optional profile picture: a base64-encoded 256x256 JPEG the
-- server normalizes on upload, plus the time it last changed (drives cache and
-- the "has avatar" read flag). Both nullable; a human without a picture falls
-- back to initials in the console.
alter table human add column if not exists avatar text;
alter table human add column if not exists avatar_updated_at timestamptz;

-- migrate:down
alter table human drop column if exists avatar_updated_at;
alter table human drop column if exists avatar;
