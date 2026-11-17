-- migrate:up
-- Client ID metadata documents (#2074). An MCP client may name itself by an
-- https URL serving its metadata instead of registering. Such a client is kept
-- as an oauth_client row keyed by that URL (metadata_url), so codes and tokens
-- link to it as to any other; fetched_at is when its document was last read,
-- which bounds how long the cached name and redirect URIs are trusted.
ALTER TABLE oauth_client ADD COLUMN IF NOT EXISTS metadata_url text;
ALTER TABLE oauth_client ADD COLUMN IF NOT EXISTS fetched_at timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS oauth_client_metadata_url_idx ON oauth_client (metadata_url) WHERE metadata_url IS NOT NULL;

-- migrate:down
DROP INDEX IF EXISTS oauth_client_metadata_url_idx;
ALTER TABLE oauth_client DROP COLUMN IF EXISTS fetched_at;
ALTER TABLE oauth_client DROP COLUMN IF EXISTS metadata_url;
