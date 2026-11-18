-- migrate:up
-- OAuth refresh tokens (#2073). An OAuth client's access token is short-lived;
-- oauth_refresh holds the refresh tokens that renew it, each a hash bound to
-- the credential row it renews. A refresh spends its token (spent_at) and
-- issues the next; the spent ones are kept until they expire so a replay is
-- recognized and ends the grant. Deleting the credential (a revoke, a new
-- sign-in, a replay) ends its refresh chain by cascade.
CREATE TABLE IF NOT EXISTS oauth_refresh (
    token_hash bytea PRIMARY KEY,
    credential_id uuid NOT NULL REFERENCES credential (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    spent_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS oauth_refresh_credential_idx ON oauth_refresh (credential_id);

-- migrate:down
DROP TABLE IF EXISTS oauth_refresh;
