-- migrate:up
-- OAuth 2.1 for MCP clients (#2048). oauth_client is a client that registered
-- itself (RFC 7591): its declared name and the exact redirect URIs a code may
-- be sent to. oauth_code is an authorization code between a person's consent
-- and the client's exchange: only its hash is kept, it carries the PKCE
-- challenge and the roles the person approved, and it is consumed once.
-- credential.oauth_client_id links the agent token an exchange mints to its
-- client, so the session list names it and a client's next token replaces
-- its last.
CREATE TABLE IF NOT EXISTS oauth_client (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_name text NOT NULL,
    redirect_uris text[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oauth_code (
    code_hash bytea PRIMARY KEY,
    client_id uuid NOT NULL REFERENCES oauth_client (id) ON DELETE CASCADE,
    principal_id uuid NOT NULL REFERENCES principal (id) ON DELETE CASCADE,
    redirect_uri text NOT NULL,
    code_challenge text NOT NULL,
    resource text NOT NULL,
    roles text[] NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE credential ADD COLUMN IF NOT EXISTS oauth_client_id uuid REFERENCES oauth_client (id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS credential_oauth_client_idx ON credential (principal_id, oauth_client_id) WHERE oauth_client_id IS NOT NULL;

-- migrate:down
DROP INDEX IF EXISTS credential_oauth_client_idx;
ALTER TABLE credential DROP COLUMN IF EXISTS oauth_client_id;
DROP TABLE IF EXISTS oauth_code;
DROP TABLE IF EXISTS oauth_client;
