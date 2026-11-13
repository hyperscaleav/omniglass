-- migrate:up

-- The receipt's ledger and outcome as integer arrays (#1699, ADR-1699). The
-- receipt is one row a batch, so its width is a fixed cost on every batch,
-- and the jsonb objects repeated every kind and outcome name on every row.
--
--   ledger  bigint[]: a (count, before) pair per kind slot, metric,
--           property, event, log; both null for a kind the ledger does not
--           declare. Null when the batch carried no ledger, as before.
--   outcome int[]:    (code, n) pairs, code = slot << 8 | outcome code, the
--           outcome codes frozen below; zeros left out, '{}' when empty.
--
-- Both lists are delivery.kindSlots and delivery.outcomeCodes, append-only,
-- pinned by TestKindSlotsArePinned and TestEveryOutcomeHasItsPinnedCode. A
-- kind or outcome name neither list knows is not carried over: no reader
-- counts it. A ledger count past int64 and an outcome count past int32 are
-- saturated at their ceilings, as delivery's Cells pack them. Each column converts only while it is still jsonb, so a second
-- run changes nothing. The conversions live in session-temporary functions:
-- they leave no object in the schema.
CREATE FUNCTION pg_temp.og_receipt_count(n text) RETURNS bigint
LANGUAGE sql IMMUTABLE STRICT AS $$
  SELECT least(n::numeric, 9223372036854775807)::bigint
$$;

CREATE FUNCTION pg_temp.og_receipt_ledger_cells(l jsonb) RETURNS bigint[]
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN l IS NULL THEN NULL ELSE ARRAY[
    pg_temp.og_receipt_count(l->'metric'->>'count'), pg_temp.og_receipt_count(l->'metric'->>'before'),
    pg_temp.og_receipt_count(l->'property'->>'count'), pg_temp.og_receipt_count(l->'property'->>'before'),
    pg_temp.og_receipt_count(l->'event'->>'count'), pg_temp.og_receipt_count(l->'event'->>'before'),
    pg_temp.og_receipt_count(l->'log'->>'count'), pg_temp.og_receipt_count(l->'log'->>'before')
  ] END
$$;

CREATE FUNCTION pg_temp.og_receipt_outcome_cells(o jsonb) RETURNS int[]
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN o IS NULL THEN NULL WHEN jsonb_typeof(o) <> 'object' THEN '{}'::int[] ELSE coalesce((
    SELECT array_agg(p.cell ORDER BY k.slot, c.code, p.part)
    FROM jsonb_each(o) AS kind(name, outcomes)
    JOIN (VALUES ('metric', 0), ('property', 1), ('event', 2), ('log', 3)) AS k(name, slot) ON k.name = kind.name
    CROSS JOIN LATERAL jsonb_each_text(CASE WHEN jsonb_typeof(kind.outcomes) = 'object' THEN kind.outcomes ELSE '{}' END) AS out(name, n)
    JOIN (VALUES
      ('stored', 1), ('coalesced', 2), ('deduplicated', 3),
      ('refused-unregistered', 4), ('refused-writer', 5), ('refused-instance', 6),
      ('refused-invalid', 7), ('refused-empty', 8), ('refused-write', 9),
      ('orphaned', 10), ('unbound', 11), ('unidentified', 12),
      ('refused-owner', 13), ('refused-task', 14), ('refused-batch', 15),
      ('refused-metadata', 16), ('abandoned', 17), ('missing', 18),
      ('shipped', 19), ('spool-overflow', 20), ('spool-expired', 21),
      ('bus-refused', 22), ('self-log-overflow', 23), ('encode-failed', 24)
    ) AS c(name, code) ON c.name = out.name
    CROSS JOIN LATERAL (VALUES (1, k.slot << 8 | c.code), (2, least(out.n::bigint, 2147483647)::int)) AS p(part, cell)
    WHERE out.n::bigint > 0
  ), '{}'::int[]) END
$$;

DO $$
BEGIN
  IF (SELECT data_type FROM information_schema.columns
      WHERE table_schema = current_schema() AND table_name = 'telemetry_receipt' AND column_name = 'ledger') = 'jsonb' THEN
    ALTER TABLE telemetry_receipt ALTER COLUMN ledger TYPE bigint[] USING pg_temp.og_receipt_ledger_cells(ledger);
  END IF;
  IF (SELECT data_type FROM information_schema.columns
      WHERE table_schema = current_schema() AND table_name = 'telemetry_receipt' AND column_name = 'outcome') = 'jsonb' THEN
    ALTER TABLE telemetry_receipt ALTER COLUMN outcome TYPE int[] USING pg_temp.og_receipt_outcome_cells(outcome);
  END IF;
END $$;

DROP FUNCTION pg_temp.og_receipt_ledger_cells(jsonb);
DROP FUNCTION pg_temp.og_receipt_outcome_cells(jsonb);
DROP FUNCTION pg_temp.og_receipt_count(text);

-- migrate:down

-- Back to the jsonb objects, by the same two lists.
CREATE FUNCTION pg_temp.og_receipt_ledger_json(l bigint[]) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN l IS NULL THEN NULL ELSE jsonb_strip_nulls(jsonb_build_object(
    'metric', CASE WHEN l[1] IS NOT NULL THEN jsonb_build_object('count', l[1], 'before', l[2]) END,
    'property', CASE WHEN l[3] IS NOT NULL THEN jsonb_build_object('count', l[3], 'before', l[4]) END,
    'event', CASE WHEN l[5] IS NOT NULL THEN jsonb_build_object('count', l[5], 'before', l[6]) END,
    'log', CASE WHEN l[7] IS NOT NULL THEN jsonb_build_object('count', l[7], 'before', l[8]) END
  )) END
$$;

CREATE FUNCTION pg_temp.og_receipt_outcome_json(o int[]) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN o IS NULL THEN NULL ELSE coalesce((
    SELECT jsonb_object_agg(per.kind, per.outcomes) FROM (
      SELECT k.name AS kind, jsonb_object_agg(c.name, o[2 * i]) AS outcomes
      FROM generate_series(1, coalesce(array_length(o, 1), 0) / 2) AS i
      JOIN (VALUES ('metric', 0), ('property', 1), ('event', 2), ('log', 3)) AS k(name, slot) ON k.slot = o[2 * i - 1] >> 8
      JOIN (VALUES
        ('stored', 1), ('coalesced', 2), ('deduplicated', 3),
        ('refused-unregistered', 4), ('refused-writer', 5), ('refused-instance', 6),
        ('refused-invalid', 7), ('refused-empty', 8), ('refused-write', 9),
        ('orphaned', 10), ('unbound', 11), ('unidentified', 12),
        ('refused-owner', 13), ('refused-task', 14), ('refused-batch', 15),
        ('refused-metadata', 16), ('abandoned', 17), ('missing', 18),
        ('shipped', 19), ('spool-overflow', 20), ('spool-expired', 21),
        ('bus-refused', 22), ('self-log-overflow', 23), ('encode-failed', 24)
      ) AS c(name, code) ON c.code = o[2 * i - 1] & 255
      GROUP BY k.name
    ) per
  ), '{}'::jsonb) END
$$;

DO $$
BEGIN
  IF (SELECT data_type FROM information_schema.columns
      WHERE table_schema = current_schema() AND table_name = 'telemetry_receipt' AND column_name = 'ledger') = 'ARRAY' THEN
    ALTER TABLE telemetry_receipt ALTER COLUMN ledger TYPE jsonb USING pg_temp.og_receipt_ledger_json(ledger);
  END IF;
  IF (SELECT data_type FROM information_schema.columns
      WHERE table_schema = current_schema() AND table_name = 'telemetry_receipt' AND column_name = 'outcome') = 'ARRAY' THEN
    ALTER TABLE telemetry_receipt ALTER COLUMN outcome TYPE jsonb USING pg_temp.og_receipt_outcome_json(outcome);
  END IF;
END $$;

DROP FUNCTION pg_temp.og_receipt_ledger_json(bigint[]);
DROP FUNCTION pg_temp.og_receipt_outcome_json(int[]);
