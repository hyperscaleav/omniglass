-- migrate:up

-- One-time data backfill (#957): event rule criteria become expressions.
-- Every stored short-form criterion {"op": ..., "value": ...} is rewritten
-- to {"expr": "..."} by the same lowering the gateway applies to a short
-- form on input (rules.Lower in internal/rules/criteria.go), so a rule
-- decides every sample after the backfill exactly as it did before
-- (TestLoweringDecidesIdentically, TestCriteriaBackfill):
--
--   a property compares text: value == "down", the threshold JSON-quoted
--     (to_json), which Expr reads back exactly;
--   a bool property compares true or false; a threshold that is neither
--     could never equal a parseable value, so eq lowers to false, ne to true;
--   a metric compares a number: value > 40.0, its JSON text made a float
--     literal so Expr parses it with the same correctly rounded ParseFloat.
--
-- No DDL; the columns stay jsonb. A criterion already holding expr, or one
-- that is not a well-formed short form, is left as it is, so a second run
-- changes nothing. The lowering lives in a session-temporary function: it
-- leaves no object in the schema.
CREATE FUNCTION pg_temp.og_lower_criteria(c jsonb, lane text, data_type text) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN c IS NULL OR jsonb_typeof(c) <> 'object' OR c ? 'expr' OR NOT (c ? 'op') OR s.sym IS NULL THEN c
    WHEN lane = 'metric' AND jsonb_typeof(c->'value') = 'number' THEN
      jsonb_build_object('expr', 'value ' || s.sym || ' ' ||
        CASE WHEN (c->'value')::text ~ '[.eE]' THEN (c->'value')::text ELSE (c->'value')::text || '.0' END)
    WHEN lane = 'property' AND jsonb_typeof(c->'value') = 'string' AND c->>'op' IN ('eq', 'ne') THEN
      CASE
        WHEN data_type = 'bool' AND c->>'value' IN ('true', 'false') THEN
          jsonb_build_object('expr', 'value ' || s.sym || ' ' || (c->>'value'))
        WHEN data_type = 'bool' THEN
          jsonb_build_object('expr', CASE c->>'op' WHEN 'eq' THEN 'false' ELSE 'true' END)
        ELSE jsonb_build_object('expr', 'value ' || s.sym || ' ' || to_json(c->>'value')::text)
      END
    ELSE c
  END
  FROM (SELECT CASE c->>'op' WHEN 'eq' THEN '==' WHEN 'ne' THEN '!=' WHEN 'gt' THEN '>'
    WHEN 'ge' THEN '>=' WHEN 'lt' THEN '<' WHEN 'le' THEN '<=' END AS sym) s
$$;

UPDATE public.event_rule r
SET fire_criteria = pg_temp.og_lower_criteria(r.fire_criteria, r.sample_lane, t.data_type),
    clear_criteria = pg_temp.og_lower_criteria(r.clear_criteria, r.sample_lane, t.data_type)
FROM (SELECT r2.id, coalesce((SELECT pt.data_type FROM public.property_type pt WHERE pt.name = r2.sample_key), 'string') AS data_type
      FROM public.event_rule r2) t
WHERE t.id = r.id AND (r.fire_criteria ? 'op' OR coalesce(r.clear_criteria ? 'op', false));

DROP FUNCTION pg_temp.og_lower_criteria(jsonb, text, text);

-- migrate:down

-- Back to the short form for the expressions the backfill (or the gateway's
-- lowering) wrote: value <op> <a JSON string, a number, true or false>. Any
-- other expression (one an operator authored, or a bool threshold lowered
-- to a bare false or true) has no short form and is left as it is; a binary
-- from before #957 reads it as a criterion it cannot evaluate, and the rule
-- holds.
CREATE FUNCTION pg_temp.og_raise_criteria(c jsonb) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN m IS NULL THEN c
    WHEN m[2] IN ('true', 'false') THEN jsonb_build_object('op', op, 'value', m[2])
    WHEN pg_input_is_valid(m[2], 'jsonb') THEN jsonb_build_object('op', op, 'value', m[2]::jsonb)
    ELSE c
  END
  FROM (SELECT regexp_match(c->>'expr',
      '^value (==|!=|>=|<=|>|<) ("(?:[^"\\]|\\.)*"|-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false)$') AS m) x,
    LATERAL (SELECT CASE x.m[1] WHEN '==' THEN 'eq' WHEN '!=' THEN 'ne' WHEN '>' THEN 'gt'
      WHEN '>=' THEN 'ge' WHEN '<' THEN 'lt' WHEN '<=' THEN 'le' END AS op) o
$$;

UPDATE public.event_rule
SET fire_criteria = pg_temp.og_raise_criteria(fire_criteria),
    clear_criteria = pg_temp.og_raise_criteria(clear_criteria)
WHERE fire_criteria ? 'expr' OR coalesce(clear_criteria ? 'expr', false);

DROP FUNCTION pg_temp.og_raise_criteria(jsonb);
