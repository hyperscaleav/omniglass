-- migrate:up

-- The conduit's inbox of unplaced identities (#1282, ADR-1282): one row per
-- conduit component, match key and value that a fanned sample named and no
-- identity binding placed, aggregated (a count, first and last heard, the
-- keys it carried), bounded per conduit and aged out, the shape of the
-- unclaimed-source inbox. Nothing about the value lands on any owner; the row
-- is what an operator binds from. Deleting the conduit or the match key's
-- type deletes its rows. The keys its samples carried are held by their
-- types' ids (a metric, property or event type: the catalogs' names are
-- disjoint and their ids unique), rendered as names on read, so a renamed
-- type is still listed.
CREATE TABLE IF NOT EXISTS unbound_identity (
  conduit_id       uuid NOT NULL REFERENCES component(id) ON DELETE CASCADE,
  property_type_id uuid NOT NULL REFERENCES property_type(id) ON DELETE CASCADE,
  value            text NOT NULL,
  heard_count      bigint NOT NULL,
  first_heard_at   timestamptz NOT NULL,
  last_heard_at    timestamptz NOT NULL,
  carried_type_ids uuid[] NOT NULL DEFAULT '{}',
  PRIMARY KEY (conduit_id, property_type_id, value)
);
CREATE INDEX IF NOT EXISTS unbound_identity_heard_idx ON unbound_identity (conduit_id, last_heard_at DESC);

-- migrate:down
DROP TABLE IF EXISTS unbound_identity;
