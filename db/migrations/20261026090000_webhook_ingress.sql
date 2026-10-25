-- migrate:up

-- The webhook transport (#1024, ADR-1024). A webhook endpoint's token is a
-- sealed secret (a webhook-token, owned by the endpoint's component, so it is
-- revealed, audited and scoped like any credential); this row binds it to its
-- endpoint and holds the token's sha256, which is how the receiver finds the
-- endpoint a post names without unsealing every token. The token is 128
-- random bits, so its digest is no oracle. The refused columns count the
-- posts the endpoint's scheme refused, by the last source and reason, flushed
-- from the listener's tally once per tick.
CREATE TABLE IF NOT EXISTS webhook_token (
  endpoint_id    uuid PRIMARY KEY REFERENCES endpoint(id) ON DELETE CASCADE,
  secret_id      uuid NOT NULL UNIQUE REFERENCES secret(id) ON DELETE CASCADE,
  token_hash     bytea NOT NULL UNIQUE,
  minted_at      timestamptz NOT NULL DEFAULT now(),
  refused_count  bigint NOT NULL DEFAULT 0,
  refused_at     timestamptz,
  refused_reason text,
  refused_source text
);

-- The unclaimed-source inbox (decision 4 of #1146): one row per source a
-- listener heard from that no endpoint claims (a post to an unknown or
-- rotated token), aggregated (a count, first and last heard, the last message
-- cut to 512 bytes), bounded per listener and aged out. host is the listener's
-- host: 'server' for the webhook route, a node's name from a later slice. No
-- token a stranger tried is ever stored.
CREATE TABLE IF NOT EXISTS unclaimed_source (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  host           text NOT NULL,
  transport      text NOT NULL,
  source         text NOT NULL,
  first_heard_at timestamptz NOT NULL,
  last_heard_at  timestamptz NOT NULL,
  heard_count    bigint NOT NULL,
  last_message   text NOT NULL DEFAULT '',
  UNIQUE (host, transport, source)
);
CREATE INDEX IF NOT EXISTS unclaimed_source_heard_idx ON unclaimed_source (host, transport, last_heard_at DESC);

-- migrate:down
DROP TABLE IF EXISTS unclaimed_source;
DROP TABLE IF EXISTS webhook_token;
