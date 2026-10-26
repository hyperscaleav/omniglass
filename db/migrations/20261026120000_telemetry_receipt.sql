-- migrate:up

-- The telemetry receipt (#311, ADR-0311): one row per ingest batch that
-- landed, written in the batch's own transaction, so a batch id lands once.
-- A JetStream redelivery (an ack that never arrived, a consumer that died
-- after its commit) and a node's retry past the stream's duplicate window
-- both carry the id of a batch that already landed, and the conflict on
-- batch_id turns them into a no-op instead of a second copy of every row.
-- batch_id is the publishing subject and the batch's Nats-Msg-Id. Receipts
-- are pruned past their horizon by landed_at (the server's retention tick),
-- so the table holds a bounded window, never the history.
CREATE TABLE IF NOT EXISTS telemetry_receipt (
  batch_id  text PRIMARY KEY,
  landed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS telemetry_receipt_landed_at_idx ON telemetry_receipt (landed_at);

-- migrate:down
DROP TABLE IF EXISTS telemetry_receipt;
